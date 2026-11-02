import dayjs from 'dayjs';

export function fmtDate(value?: string | null): string {
  return value ? dayjs(value).format('YYYY-MM-DD') : '—';
}

export function fmtDateTime(value?: string | null): string {
  return value ? dayjs(value).format('YYYY-MM-DD HH:mm') : '—';
}

export function fmtShort(value?: string | null): string {
  return value ? dayjs(value).format('MM-DD') : '—';
}

export function fromNow(value?: string | null): string {
  return value ? dayjs(value).fromNow() : '—';
}

export function todayISO(): string {
  return dayjs().format('YYYY-MM-DD');
}

export function downloadFile(filename: string, content: BlobPart, type = 'text/plain;charset=utf-8'): void {
  const blob = content instanceof Blob ? content : new Blob([content], { type });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

export function toCsv(rows: Array<Array<string | number | null | undefined>>): string {
  const escape = (value: string | number | null | undefined): string => {
    const text = value === null || value === undefined ? '' : String(value);
    return /[",\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
  };
  return rows.map((row) => row.map(escape).join(',')).join('\r\n');
}

export function clampPercent(value: number | null | undefined): number {
  if (!value || value < 0) return 0;
  if (value > 100) return 100;
  return Math.round(value);
}
