import { useEffect, useRef, useState } from 'react';
import type { BotDataTable } from '../types';

type StatsGridProps = {
  tables: BotDataTable[];
  stats: Record<string, number>;
  selectedTable: string;
  onSelectTable: (name: string) => void;
};

const prefersReducedMotion = () =>
  window.matchMedia('(prefers-reduced-motion: reduce)').matches;

const useCountUp = (target: number, duration = 650) => {
  const [display, setDisplay] = useState(target);
  const previous = useRef(target);

  useEffect(() => {
    if (prefersReducedMotion() || previous.current === target) {
      previous.current = target;
      setDisplay(target);
      return;
    }
    const from = previous.current;
    const started = performance.now();
    let frame = 0;
    const tick = (now: number) => {
      const progress = Math.min(1, (now - started) / duration);
      const eased = 1 - (1 - progress) ** 3;
      setDisplay(Math.round(from + (target - from) * eased));
      if (progress < 1) {
        frame = requestAnimationFrame(tick);
      } else {
        previous.current = target;
      }
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [target, duration]);

  return display;
};

const StatNumber = ({ value }: { value: number }) => {
  const display = useCountUp(value);
  return <strong>{display}</strong>;
};

export const StatsGrid = ({
  tables,
  stats,
  selectedTable,
  onSelectTable,
}: StatsGridProps) => (
  <section className="stats-grid">
    {tables.slice(0, 6).map((table) => (
      <button
        aria-pressed={selectedTable === table.name}
        className={`card stat-card${selectedTable === table.name ? ' selected' : ''}`}
        key={table.name}
        onClick={() => onSelectTable(table.name)}
        type="button"
      >
        <span>{table.label}</span>
        <StatNumber value={stats[table.name] ?? 0} />
        <small>{table.description}</small>
      </button>
    ))}
  </section>
);
