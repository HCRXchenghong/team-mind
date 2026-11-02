import { ref, watch } from 'vue';

const KEY = 'teammind.theme';

export const isDark = ref(localStorage.getItem(KEY) === 'dark');

function apply(dark: boolean): void {
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
}

apply(isDark.value);

watch(isDark, (dark) => {
  localStorage.setItem(KEY, dark ? 'dark' : 'light');
  apply(dark);
});

export function toggleTheme(): void {
  isDark.value = !isDark.value;
}

export function setTheme(dark: boolean): void {
  isDark.value = dark;
}
