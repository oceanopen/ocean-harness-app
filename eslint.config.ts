import antfu from '@antfu/eslint-config';

export default await antfu(
  {
    ignores: [
      '**/*.yaml',
      '**/**.yml',
      'src-tauri',
      'src-server',
      'dist',
      'node_modules',
      'src/shared/bindings.ts',
      'docs',
    ],
    stylistic: {
      indent: 2,
      quotes: 'single',
      semi: true,
    },
    formatters: {
      prettierOptions: {
        tabWidth: 2,
        useTabs: false,
        trailingComma: 'all',
        singleQuote: true,
        semi: true,
      },
      css: true,
      html: true,
      markdown: 'prettier',
    },
    typescript: true,
    react: true,
    rules: {
      'curly': 'error',
      'style/max-statements-per-line': ['error', { max: 1 }],
      'style/brace-style': ['error', '1tbs'],
      'no-console': 'off',
      'style/jsx-one-expression-per-line': 'off',
      'react/exhaustive-deps': 'off',
    },
  },
  {
    files: ['**/*.md'],
    rules: {
      'markdown/require-alt-text': 'off',
      'markdown/no-multiple-h1': 'off',
      'markdown/no-empty-links': 'off',
      'markdown/heading-increment': 'off',
    },
  },
);
