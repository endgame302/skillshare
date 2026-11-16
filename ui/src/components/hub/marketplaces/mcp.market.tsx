// Fork-only (endgame302/skillshare): registers the MCP servers marketplace on the Hubs page.
import { Server } from 'lucide-react';
import type { Marketplace } from './registry';
import MCPMarketView from './mcp/MCPMarketView';

export const marketplace: Marketplace = {
  id: 'mcp',
  order: 60,
  labelKey: 'mcpmarket.market.label',
  subKey: 'mcpmarket.market.sub',
  icon: Server,
  View: MCPMarketView,
};
