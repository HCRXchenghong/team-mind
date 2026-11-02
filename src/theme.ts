import { computed } from 'vue';
import { theme } from 'ant-design-vue';
import zhCN from 'ant-design-vue/es/locale/zh_CN';
import { isDark } from '@/composables/useTheme';

const dark = isDark;

export const themeConfig = computed(() => {
  const isDarkMode = dark.value;
  return {
    algorithm: isDarkMode ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      colorPrimary: '#2f54eb',
      colorInfo: '#2f54eb',
      borderRadius: 6,
      fontSize: 13,
      controlHeight: 32,
      wireframe: false,
      colorBgLayout: isDarkMode ? '#0c0e13' : '#f4f5f8',
      colorBgContainer: isDarkMode ? '#141821' : '#ffffff',
      colorBgElevated: isDarkMode ? '#1a1f2b' : '#ffffff',
      colorBorderSecondary: isDarkMode ? '#252b38' : '#e6e8ef',
      colorText: isDarkMode ? '#e7eaf2' : '#1b1f27',
      colorTextSecondary: isDarkMode ? '#b7bfd0' : '#4b5363',
      colorTextTertiary: isDarkMode ? '#8791a5' : '#7a8291',
    },
    components: {
      Menu: {
        itemBg: 'transparent',
        subMenuItemBg: 'transparent',
        itemHeight: 34,
        itemMarginInline: 8,
        itemBorderRadius: 6,
        itemColor: isDarkMode ? '#b7bfd0' : '#4b5363',
        itemHoverBg: isDarkMode ? '#1a1f2b' : '#f2f4f8',
        itemSelectedBg: isDarkMode ? 'rgba(109,139,255,.16)' : '#eef1ff',
        itemSelectedColor: '#2f54eb',
        groupTitleColor: isDarkMode ? '#6f788b' : '#98a0ae',
        groupTitleFontSize: 11,
      },
      Table: {
        headerBg: isDarkMode ? '#1a1f2b' : '#f7f8fa',
        headerColor: isDarkMode ? '#8791a5' : '#7a8291',
        headerSplitColor: 'transparent',
        rowHoverBg: isDarkMode ? '#181d27' : '#fafbfd',
        borderColor: isDarkMode ? '#252b38' : '#eef0f4',
        fontSize: 12.5,
      },
      Card: {
        colorBgContainer: isDarkMode ? '#141821' : '#ffffff',
      },
    },
  };
});
