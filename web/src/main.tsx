import React, {lazy,Suspense} from 'react';
import { createRoot } from 'react-dom/client';
import App from './App';
import './styles.css';

const Admin=lazy(()=>import('./components/Admin'));
createRoot(document.getElementById('root')!).render(<React.StrictMode>{location.pathname==='/admin'?<Suspense fallback={<p>正在加载管理后台…</p>}><Admin/></Suspense>:<App/>}</React.StrictMode>);
