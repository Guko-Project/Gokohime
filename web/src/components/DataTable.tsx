import { useMemo } from 'react';
import { formatValue, visibleColumns } from '../api';
import type { BotDataTable, RowsResponse } from '../types';

type DataTableProps = {
  tables: BotDataTable[];
  selectedTable: string;
  rows: RowsResponse | null;
  onSelectTable: (name: string) => void;
};

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
  );
};
