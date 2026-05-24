export type ApiError = {
  error?: {
    code: string;
    message: string;
  };
};

export type AdminUser = {
  username: string;
  display_name: string;
  role: string;
};

export type BotDataTable = {
  name: string;
  label: string;
  description: string;
};

export type StatsResponse = {
  tables: Record<string, number>;
};

export type TablesResponse = {
  tables: BotDataTable[];
};

export type RowsResponse = {
  table: BotDataTable;
  total: number;
  limit: number;
  offset: number;
  rows: Record<string, unknown>[];
};

export type ConfigSummary = {
  bot: Record<string, unknown>;
  admin: Record<string, unknown>;
  database: Record<string, unknown>;
};
