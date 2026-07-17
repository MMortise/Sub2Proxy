import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { KeyRound, Network } from 'lucide-react'
import { api, errMessage } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ThemeToggle } from '@/components/theme-toggle'

export function LoginPage() {
  const navigate = useNavigate()
  const [key, setKey] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!key.trim()) {
      setError('请输入访问密钥')
      return
    }
    setLoading(true)
    setError('')
    try {
      await api.login(key)
      navigate('/subscriptions', { replace: true })
    } catch (err) {
      // The server returns actionable messages (remaining attempts / lockout).
      setError(errMessage(err, '登录失败'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="relative flex min-h-screen items-center justify-center bg-background px-4">
      <div className="absolute right-4 top-4">
        <ThemeToggle />
      </div>
      <Card className="w-full max-w-sm">
        <CardHeader className="items-center text-center">
          <div className="mb-2 flex h-12 w-12 items-center justify-center rounded-xl bg-primary text-primary-foreground">
            <Network className="h-6 w-6" />
          </div>
          <CardTitle className="text-xl">sub2proxy 管理面板</CardTitle>
          <CardDescription>请输入访问密钥以登录</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="key">访问密钥</Label>
              <div className="relative">
                <KeyRound className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  id="key"
                  type="password"
                  autoFocus
                  autoComplete="current-password"
                  placeholder="••••••••"
                  className="pl-9"
                  value={key}
                  onChange={(e) => {
                    setKey(e.target.value)
                    if (error) setError('')
                  }}
                />
              </div>
              {error && <p className="text-sm text-destructive">{error}</p>}
            </div>
            <Button type="submit" className="w-full" loading={loading}>
              登录
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
