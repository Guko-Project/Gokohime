import { useMemo } from 'react';
import { formatValue, visibleColumns } from '../api';
import type { BotDataTable, RowsResponse } from '../types';

type DataTableProps = {
  tables: BotDataTable[];
  selectedTable: string;
  rows: RowsResponse | null;
  onSelectTable: (name: string) => void;
};

const SkeletonRows = ({ columns }: { columns: number }) => (
  <>
    {[0, 1, 2, 3, 4].map((row) => (
      <tr className="skel-row" key={row}>
        {Array.from({ length: columns }, (_, i) => `cell-${i}`).map(
          (cell, i) => (
            <td key={cell}>
              <span
                className="skel"
                style={{ width: `${55 + ((row * 17 + i * 29) % 40)}%` }}
              />
            </td>
          ),
        )}
      </tr>
    ))}
  </>
);

export const DataTable = ({
  tables,
  selectedTable,
  rows,
  onSelectTable,
}: DataTableProps) => {
  const activeTable = useMemo(
    () => tables.find((table) => table.name === selectedTable),
    [tables, selectedTable],
  );
  const columns = rows ? visibleColumns(rows.rows) : [];
  const loading = rows === null;

  return (
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
          aria-label="选择数据表"
          value={selectedTable}
          onChange={(event) => onSelectTable(event.target.value)}
        >
          {tables.map((table) => (
            <option key={table.name} value={table.name}>
              {table.label}
            </option>
          ))}
        </select>
      </div>
      <div className="table-wrap" aria-busy={loading}>
        <table>
          <thead>
            <tr>
              {(loading ? ['列'] : columns).map((column) => (
                <th key={column}>{column}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <SkeletonRows columns={1} />
            ) : (
              rows.rows.map((row) => (
                <tr key={`${selectedTable}-${formatValue(row.id)}`}>
                  {columns.map((column) => (
                    <td key={column} title={formatValue(row[column])}>
                      {formatValue(row[column])}
                    </td>
                  ))}
                </tr>
              ))
            )}
          </tbody>
        </table>
        {rows?.rows.length === 0 ? <p className="empty">暂无数据</p> : null}
      </div>
    </section>
  );
};
