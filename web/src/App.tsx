import { useEffect, useState } from 'react';
import './App.css';
import { request, tokenKey } from './api';
import { ConfigSummary } from './components/ConfigSummary';
import { DashboardHero } from './components/DashboardHero';
import { DataTable } from './components/DataTable';
import { LoginPage } from './components/LoginPage';
import { Sidebar } from './components/Sidebar';
import { StatsGrid } from './components/StatsGrid';
import type {
  AdminUser,
  BotDataTable,
  ConfigSummary as ConfigSummaryType,
  RowsResponse,
  StatsResponse,
  TablesResponse,
} from './types';

const App = () => {
  const [token, setToken] = useState(
    () => localStorage.getItem(tokenKey) ?? '',
  );
  const [user, setUser] = useState<AdminUser | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [tables, setTables] = useState<BotDataTable[]>([]);
  const [stats, setStats] = useState<Record<string, number>>({});
  const [configSummary, setConfigSummary] = useState<ConfigSummaryType | null>(
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
          request<ConfigSummaryType>('/api/system/config-summary', token),
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
      setRows(null);
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

  const login = async (username: string, password: string) => {
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
    return <LoginPage onLogin={login} error={error} loading={loading} />;
  }

  return (
    <main className="app-shell">
      <Sidebar user={user} onLogout={logout} />
      <section className="workspace">
        {error ? <div className="alert">{error}</div> : null}
        <DashboardHero
          driver={String(configSummary?.database?.driver ?? 'sqlite')}
        />
        <StatsGrid
          tables={tables}
          stats={stats}
          selectedTable={selectedTable}
          onSelectTable={setSelectedTable}
        />
        <DataTable
          tables={tables}
          selectedTable={selectedTable}
          rows={rows}
          onSelectTable={setSelectedTable}
        />
        <ConfigSummary config={configSummary} />
      </section>
    </main>
  );
};

export default App;
