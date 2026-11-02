import { message } from 'ant-design-vue';

export function errorText(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === 'string') return error;
  return '操作失败，请稍后重试';
}

export const notify = {
  success(text: string): void {
    message.success(text);
  },
  info(text: string): void {
    message.info(text);
  },
  warning(text: string): void {
    message.warning(text);
  },
  error(error: unknown): void {
    message.error(errorText(error));
  },
};
