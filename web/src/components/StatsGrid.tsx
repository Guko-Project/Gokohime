import type { BotDataTable } from '../types';

type StatsGridProps = {
  tables: BotDataTable[];
  stats: Record<string, number>;
  onSelectTable: (name: string) => void;
};

export const StatsGrid = ({ tables, stats, onSelectTable }: StatsGridProps) => (
  <section className="stats-grid">
    {tables.slice(0, 6).map((table) => (
      <button
        className="card stat-card"
        key={table.name}
        onClick={() => onSelectTable(table.name)}
        type="button"
      >
        <span>{table.label}</span>
        <strong>{stats[table.name] ?? 0}</strong>
        <small>{table.description}</small>
      </button>
    ))}
  </section>
);
