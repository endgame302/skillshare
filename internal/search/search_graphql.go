package search

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	ghclient "skillshare/internal/github"
)

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type graphQLResponse struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []json.RawMessage          `json:"errors"`
}

type blobMetadataHit struct {
	meta skillMetadata
	oid  string
}

func doGraphQL(client *http.Client, token string, reqBody graphQLRequest) (map[string]json.RawMessage, error) {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	apiURL := strings.TrimRight(githubAPIBase, "/") + "/graphql"
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := ghclient.CheckRateLimit(resp); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub GraphQL API returned %d", resp.StatusCode)
	}

	var gqlResp graphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		return nil, err
	}
	if gqlResp.Data == nil {
		return nil, fmt.Errorf("GitHub GraphQL response missing data")
	}

	return gqlResp.Data, nil
}

func isJSONNull(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

func fetchRepoStarsGraphQL(client *http.Client, token string, repos []repoID) (map[repoID]int, error) {
	if len(repos) == 0 {
		return nil, nil
	}

	decls := make([]string, 0, len(repos))
	fields := make([]string, 0, len(repos))
	vars := make(map[string]any, len(repos)*2)

	for i, id := range repos {
		decls = append(decls, fmt.Sprintf("$o%d: String!, $n%d: String!", i, i))
		fields = append(fields, fmt.Sprintf("r%d: repository(owner: $o%d, name: $n%d) { stargazerCount }", i, i, i))
		vars[fmt.Sprintf("o%d", i)] = id.owner
		vars[fmt.Sprintf("n%d", i)] = id.repo
	}

	query := fmt.Sprintf("query(%s) { %s }", strings.Join(decls, ", "), strings.Join(fields, " "))
	data, err := doGraphQL(client, token, graphQLRequest{
		Query:     query,
		Variables: vars,
	})
	if err != nil {
		return nil, err
	}

	stars := make(map[repoID]int, len(repos))
	for i, id := range repos {
		raw, ok := data[fmt.Sprintf("r%d", i)]
		if !ok || isJSONNull(raw) {
			continue
		}
		var node struct {
			StargazerCount *int `json:"stargazerCount"`
		}
		if err := json.Unmarshal(raw, &node); err != nil || node.StargazerCount == nil {
			continue
		}
		stars[id] = *node.StargazerCount
	}

	return stars, nil
}

func fetchSkillMetadataGraphQLBatch(client *http.Client, token string, items []skillMetaTarget) (map[int]blobMetadataHit, error) {
	if len(items) == 0 {
		return nil, nil
	}

	decls := make([]string, 0, len(items))
	fields := make([]string, 0, len(items))
	vars := make(map[string]any, len(items)*3)

	for i, item := range items {
		decls = append(decls, fmt.Sprintf("$o%d: String!, $n%d: String!, $e%d: String!", i, i, i))
		fields = append(fields, fmt.Sprintf("b%d: repository(owner: $o%d, name: $n%d) { object(expression: $e%d) { ... on Blob { oid text isBinary } } }", i, i, i, i))
		vars[fmt.Sprintf("o%d", i)] = item.key.owner
		vars[fmt.Sprintf("n%d", i)] = item.key.repo
		vars[fmt.Sprintf("e%d", i)] = "HEAD:" + skillPathForDir(item.key.path)
	}

	query := fmt.Sprintf("query(%s) { %s }", strings.Join(decls, ", "), strings.Join(fields, " "))
	data, err := doGraphQL(client, token, graphQLRequest{
		Query:     query,
		Variables: vars,
	})
	if err != nil {
		return nil, err
	}

	hits := make(map[int]blobMetadataHit, len(items))
	for i := range items {
		raw, ok := data[fmt.Sprintf("b%d", i)]
		if !ok || isJSONNull(raw) {
			continue
		}
		var node struct {
			Object *struct {
				OID      string  `json:"oid"`
				Text     *string `json:"text"`
				IsBinary bool    `json:"isBinary"`
			} `json:"object"`
		}
		if err := json.Unmarshal(raw, &node); err != nil {
			continue
		}
		if node.Object == nil || node.Object.Text == nil || node.Object.IsBinary {
			continue
		}
		hits[i] = blobMetadataHit{
			meta: parseSkillMetadata(*node.Object.Text),
			oid:  node.Object.OID,
		}
	}

	return hits, nil
}
