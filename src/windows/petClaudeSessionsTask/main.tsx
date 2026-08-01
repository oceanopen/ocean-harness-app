import AppI18nProvider from '@src/shared/AppI18nProvider';
import AppThemeProvider from '@src/shared/AppThemeProvider';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import PetClaudeSessionsTaskApp from './PetClaudeSessionsTaskApp';
import './index.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppThemeProvider>
      <AppI18nProvider>
        <PetClaudeSessionsTaskApp />
      </AppI18nProvider>
    </AppThemeProvider>
  </StrictMode>,
);
