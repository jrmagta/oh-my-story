import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createBrowserRouter, Navigate, RouterProvider } from 'react-router'
import './app.css'
import { ApiError } from './api'
import { AppShell, RequireAuth, WorkspaceRoute, WorkspacesPage } from './pages/app'
import { LoginPage, NotFoundPage, SetupPage } from './pages/auth'
import { InvitePage } from './pages/invite'
import { MembersPage } from './pages/members'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      // A 4xx will not fix itself on retry.
      retry: (n, e) => !(e instanceof ApiError && e.status < 500) && n < 2,
    },
  },
})

const router = createBrowserRouter([
  { path: '/login', element: <LoginPage /> },
  { path: '/setup', element: <SetupPage /> },
  { path: '/invites/:token', element: <InvitePage /> },
  {
    element: <RequireAuth />,
    children: [
      {
        element: <AppShell />,
        children: [
          { path: '/', element: <WorkspacesPage /> },
          {
            path: '/w/:slug',
            element: <WorkspaceRoute />,
            children: [
              { index: true, element: <Navigate to="members" replace /> },
              { path: 'members', element: <MembersPage /> },
            ],
          },
        ],
      },
    ],
  },
  { path: '*', element: <NotFoundPage /> },
])

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
)
