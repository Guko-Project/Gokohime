import { useEffect, useMemo, useState } from 'react';
import './App.css';

type ApiError = {
  error?: {
    code: string;
    message: string;
  };
};

type AdminUser = {
  username: string;
  display_name: string;
  role: string;
};

type BotDataTable = {
  name: string;
  label: string;
  description: string;
};

type StatsResponse = {
  tables: Record<string, number>;
};

type TablesResponse = {
  tables: BotDataTable[];
};

type RowsResponse = {
  table: BotDataTable;
  total: number;
  limit: number;
  offset: number;
  rows: Record<string, unknown>[];
};

type ConfigSummary = {
  bot: Record<string, unknown>;
  admin: Record<string, unknown>;
  database: Record<string, unknown>;
};

const tokenKey = 'gokohime_admin_token';

async function request<T>(
  path: string,
  token?: string,
  init?: RequestInit,
): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set('Content-Type', 'application/json');
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }
  const response = await fetch(path, { ...init, headers });
  const data = (await response.json().catch(() => ({}))) as T & ApiError;
  if (!response.ok) {
    throw new Error(
      data.error?.message ?? `Request failed: ${response.status}`,
    );
  }
  return data;
}

const formatValue = (value: unknown): string => {
  if (value === null || value === undefined) {
    return '';
  }
  if (typeof value === 'object') {
    return JSON.stringify(value);
  }
  return String(value);
};

const visibleColumns = (rows: Record<string, unknown>[]): string[] => {
  const preferred = [
    'id',
    'created_at',
    'updated_at',
    'name',
    'kind',
    'category',
    'key',
    'value',
    'text',
  ];
  const discovered = Array.from(
    new Set(rows.flatMap((row) => Object.keys(row))),
  );
  const ordered = preferred.filter((key) => discovered.includes(key));
  const rest = discovered.filter((key) => !ordered.includes(key));
  return [...ordered, ...rest].slice(0, 8);
};

const App = () => {
  const [token, setToken] = useState(
    () => localStorage.getItem(tokenKey) ?? '',
  );
  const [user, setUser] = useState<AdminUser | null>(null);
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [tables, setTables] = useState<BotDataTable[]>([]);
  const [stats, setStats] = useState<Record<string, number>>({});
  const [configSummary, setConfigSummary] = useState<ConfigSummary | null>(
    null,
  );
  const [selectedTable, setSelectedTable] = useState('');
  const [rows, setRows] = useState<RowsResponse | null>(null);

  useEffect(() => {
    if (!token) {
      return;
    }
    const load = async () => {
      try {
        const me = await request<{ user: AdminUser }>('/api/auth/me', token);
        const [tablesData, statsData, configData] = await Promise.all([
          request<TablesResponse>('/api/bot-data/tables', token),
          request<StatsResponse>('/api/system/stats', token),
          request<ConfigSummary>('/api/system/config-summary', token),
        ]);
        setUser(me.user);
        setTables(tablesData.tables);
        setStats(statsData.tables);
        setConfigSummary(configData);
        setSelectedTable(
          (current) => current || tablesData.tables[0]?.name || '',
        );
      } catch (err) {
        setError(err instanceof Error ? err.message : '加载失败');
        setToken('');
        localStorage.removeItem(tokenKey);
      }
    };
    void load();
  }, [token]);

  useEffect(() => {
    if (!token || !selectedTable) {
      return;
    }
    const loadRows = async () => {
      try {
        const data = await request<RowsResponse>(
          `/api/bot-data/tables/${selectedTable}?limit=50`,
          token,
        );
        setRows(data);
      } catch (err) {
        setError(err instanceof Error ? err.message : '加载表数据失败');
      }
    };
    void loadRows();
  }, [token, selectedTable]);

  const activeTable = useMemo(
    () => tables.find((table) => table.name === selectedTable),
    [tables, selectedTable],
  );
  const columns = rows ? visibleColumns(rows.rows) : [];

  const login = async (event: React.FormEvent) => {
    event.preventDefault();
    setLoading(true);
    setError('');
    try {
      const data = await request<{ token: string; user: AdminUser }>(
        '/api/auth/login',
        undefined,
        {
          method: 'POST',
          body: JSON.stringify({ username, password }),
        },
      );
      localStorage.setItem(tokenKey, data.token);
      setToken(data.token);
      setUser(data.user);
      setPassword('');
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败');
    } finally {
      setLoading(false);
    }
  };

  const logout = () => {
    localStorage.removeItem(tokenKey);
    setToken('');
    setUser(null);
    setRows(null);
  };

  if (!token || !user) {
    return (
      <main className="login-page">
        <form className="card login-card" onSubmit={login}>
          <div>
            <p className="eyebrow">Gokohime Admin</p>
            <h1>登录管理端</h1>
            <p className="muted">使用配置中的初始管理员账号进入后台。</p>
          </div>
          <label>
            用户名
            <input
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              autoComplete="username"
            />
          </label>
          <label>
            密码
            <input
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              type="password"
              autoComplete="current-password"
            />
          </label>
          {error ? <p className="error">{error}</p> : null}
          <button className="button" disabled={loading} type="submit">
            {loading ? '登录中...' : '登录'}
          </button>
        </form>
      </main>
    );
  }

  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div>
          <p className="eyebrow">Gokohime</p>
          <h1>Admin</h1>
        </div>
        <nav>
          <a href="#dashboard">Dashboard</a>
          <a href="#bot-data">Bot 数据</a>
          <a href="#system">系统配置</a>
        </nav>
        <button className="button ghost" onClick={logout} type="button">
          退出 {user.username}
        </button>
      </aside>

      <section className="workspace">
        {error ? <div className="alert">{error}</div> : null}

        <section className="hero" id="dashboard">
          <div>
            <p className="eyebrow">Dashboard</p>
            <h2>Bot 和 Admin 已连接同一个 SQLite</h2>
            <p className="muted">V1 聚焦现有 Bot 数据管理。</p>
          </div>
          <div className="badge">
            {configSummary?.database?.driver ?? 'sqlite'}
          </div>
        </section>

        <section className="stats-grid">
          {tables.slice(0, 6).map((table) => (
            <button
              className="card stat-card"
              key={table.name}
              onClick={() => setSelectedTable(table.name)}
              type="button"
            >
              <span>{table.label}</span>
              <strong>{stats[table.name] ?? 0}</strong>
              <small>{table.description}</small>
            </button>
          ))}
        </section>

        <section className="card" id="bot-data">
          <div className="section-header">
            <div>
              <p className="eyebrow">Bot Data</p>
              <h2>{activeTable?.label ?? '数据表'}</h2>
              <p className="muted">
                {activeTable?.description ?? '选择一个表查看最近数据。'}
              </p>
            </div>
            <select
              value={selectedTable}
              onChange={(event) => setSelectedTable(event.target.value)}
            >
              {tables.map((table) => (
                <option key={table.name} value={table.name}>
                  {table.label}
                </option>
              ))}
            </select>
          </div>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  {columns.map((column) => (
                    <th key={column}>{column}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows?.rows.map((row) => (
                  <tr key={`${selectedTable}-${formatValue(row.id)}`}>
                    {columns.map((column) => (
                      <td key={column}>{formatValue(row[column])}</td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
            {rows?.rows.length === 0 ? <p className="empty">暂无数据</p> : null}
          </div>
        </section>

        <section className="card" id="system">
          <div className="section-header">
            <div>
              <p className="eyebrow">System</p>
              <h2>配置摘要</h2>
            </div>
          </div>
          <pre>{JSON.stringify(configSummary, null, 2)}</pre>
        </section>
      </section>
    </main>
  );
};

export default App;
