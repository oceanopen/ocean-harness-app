import AppI18nProvider from '@src/shared/AppI18nProvider';
import AppThemeProvider from '@src/shared/AppThemeProvider';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import PetClaudeSessionsSummaryApp from './PetClaudeSessionsSummaryApp';
import './index.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppThemeProvider>
      <AppI18nProvider>
        <PetClaudeSessionsSummaryApp />
      </AppI18nProvider>
    </AppThemeProvider>
  </StrictMode>,
);
