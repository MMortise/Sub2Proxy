import { useEffect } from 'react'
import { Navigate, Route, Routes, useNavigate } from 'react-router-dom'
import { TooltipProvider } from '@/components/ui/tooltip'
import { Toaster } from '@/components/ui/sonner'
import { Layout } from '@/components/layout'
import { setUnauthorizedHandler } from '@/lib/api'
import { LoginPage } from '@/pages/login'
import { SubscriptionsPage } from '@/pages/subscriptions'
import { NodesPage } from '@/pages/nodes'
import { MappingsPage } from '@/pages/mappings'
import { StatusPage } from '@/pages/status'

export function App() {
  const navigate = useNavigate()

  // Any 401 from the API redirects to the login page.
  useEffect(() => {
    setUnauthorizedHandler(() => {
      if (window.location.pathname !== '/login') {
        navigate('/login', { replace: true })
      }
    })
    return () => setUnauthorizedHandler(null)
  }, [navigate])

  return (
    <TooltipProvider delayDuration={200}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route element={<Layout />}>
          <Route index element={<Navigate to="/subscriptions" replace />} />
          <Route path="/subscriptions" element={<SubscriptionsPage />} />
          <Route path="/nodes" element={<NodesPage />} />
          <Route path="/mappings" element={<MappingsPage />} />
          <Route path="/status" element={<StatusPage />} />
        </Route>
        <Route path="*" element={<Navigate to="/subscriptions" replace />} />
      </Routes>
      <Toaster />
    </TooltipProvider>
  )
}
