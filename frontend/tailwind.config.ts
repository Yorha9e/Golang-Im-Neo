import type { Config } from 'tailwindcss';

export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        sand: {
          cream: '#FAF8F5',
          sand: '#F0EBE3',
          wheat: '#C9B99A',
          walnut: '#8B7355',
          dark: '#4A3B2C',
          darker: '#2E2419',
          muted: '#A3927C',
          light: '#F5F0E8',
          border: 'rgba(201, 185, 154, 0.4)',
        },
      },
      boxShadow: {
        'warm-sm': '0 2px 8px rgba(139, 115, 85, 0.06)',
        'warm-md': '0 8px 24px rgba(139, 115, 85, 0.08)',
        'warm-lg': '0 16px 40px rgba(139, 115, 85, 0.12)',
        'warm-glow': '0 0 25px rgba(201, 185, 154, 0.35)',
        'walnut-glow': '0 4px 20px rgba(139, 115, 85, 0.25)',
      },
      fontFamily: {
        sans: ['Inter', 'Outfit', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'sans-serif'],
        mono: ['JetBrains Mono', 'Fira Code', 'monospace'],
      },
      animation: {
        'pulse-subtle': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'ripple': 'ripple 1.5s cubic-bezier(0, 0.2, 0.8, 1) infinite',
      },
      keyframes: {
        ripple: {
          '0%': { transform: 'scale(0.8)', opacity: '1' },
          '100%': { transform: 'scale(2.2)', opacity: '0' },
        },
      },
    },
  },
  plugins: [],
} satisfies Config;
