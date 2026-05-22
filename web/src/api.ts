import type { ApiError } from './types';

export const tokenKey = 'gokohime_admin_token';

export async function request<T>(
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

export const formatValue = (value: unknown): string => {
  if (value === null || value === undefined) {
    return '';
  }
  if (typeof value === 'object') {
    return JSON.stringify(value);
  }
  return String(value);
};

export const visibleColumns = (rows: Record<string, unknown>[]): string[] => {
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
