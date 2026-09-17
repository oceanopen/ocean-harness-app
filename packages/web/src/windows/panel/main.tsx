import AppI18nProvider from '@src/shared/AppI18nProvider';
import AppThemeProvider from '@src/shared/AppThemeProvider';
import { queryClient } from '@src/state/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { HashRouter } from 'react-router-dom';
import PanelApp from './PanelApp';
import './index.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppThemeProvider>
      <AppI18nProvider>
        <QueryClientProvider client={queryClient}>
          <HashRouter>
            <PanelApp />
          </HashRouter>
        </QueryClientProvider>
      </AppI18nProvider>
    </AppThemeProvider>
  </StrictMode>,
);
