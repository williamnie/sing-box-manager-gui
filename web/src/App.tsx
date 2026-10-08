import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { lazy, Suspense } from 'react';
import Layout from './components/Layout';
import Dashboard from './pages/Dashboard';
import Subscriptions from './pages/Subscriptions';
import Rules from './pages/Rules';
import Settings from './pages/Settings';
import Logs from './pages/Logs';
import { ToastContainer } from './components/Toast';
import AuthGate from './components/AuthGate';
const Gateway = lazy(() => import('./pages/Gateway'));
const ConfigurationImport = lazy(() => import('./pages/ConfigurationImport'));
const Configuration = lazy(() => import('./pages/Configuration'));
const Proxies = lazy(() => import('./pages/Proxies'));
const Connections = lazy(() => import('./pages/Connections'));
const DNSQueries = lazy(() => import('./pages/DNSQueries'));

function App() {
  return (
    <BrowserRouter>
      <ToastContainer />
      <AuthGate><Layout>
        <Suspense fallback={<p className="text-default-500">正在加载管理页面…</p>}><Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/subscriptions" element={<Subscriptions />} />
          <Route path="/proxies" element={<Proxies />} />
          <Route path="/connections" element={<Connections />} />
          <Route path="/dns-queries" element={<DNSQueries />} />
          <Route path="/rules" element={<Rules />} />
          <Route path="/logs" element={<Logs />} />
          <Route path="/settings" element={<Settings />} />
          <Route path="/gateway" element={<Gateway />} />
          <Route path="/migration" element={<Navigate to="/configuration/import" replace />} />
          <Route path="/configuration/import" element={<ConfigurationImport />} />
          <Route path="/configuration" element={<Configuration />} />
        </Routes></Suspense>
      </Layout></AuthGate>
    </BrowserRouter>
  );
}

export default App;
