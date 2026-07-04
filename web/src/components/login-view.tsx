import { useState, type FormEvent } from "react"
import { Loader2Icon, PlayIcon } from "lucide-react"
import { toast } from "sonner"

import { login, register, storeSession, type User } from "@/lib/api"
import { Aurora } from "@/components/aurora"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"

interface LoginViewProps {
  onLogin: (user: User) => void
}

export function LoginView({ onLogin }: LoginViewProps) {
  const [mode, setMode] = useState<"login" | "register">("login")
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (busy) return
    setBusy(true)
    try {
      if (mode === "register") {
        await register(username, password)
        toast.success("Account created — signing you in")
      }
      const res = await login(username, password)
      storeSession(res.token, res.user)
      onLogin(res.user)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Something went wrong")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="relative flex min-h-svh items-center justify-center p-6">
      <Aurora />

      <div className="w-full max-w-sm animate-fade-up">
        <div className="mb-8 flex flex-col items-center gap-3">
          <div className="flex size-14 items-center justify-center rounded-2xl bg-gradient-to-br from-violet-500 to-fuchsia-500 shadow-lg shadow-violet-500/25">
            <PlayIcon className="size-7 fill-white text-white" />
          </div>
          <h1 className="text-gradient text-3xl font-semibold tracking-tight">
            outofmatrix
          </h1>
          <p className="text-sm text-muted-foreground">
            Your photos, music and video — on your own hardware.
          </p>
        </div>

        <Card className="border-white/10 bg-card/60 backdrop-blur-xl">
          <CardHeader>
            <Tabs
              value={mode}
              onValueChange={(v) => setMode(v as "login" | "register")}
            >
              <TabsList className="w-full">
                <TabsTrigger value="login" className="flex-1">
                  Sign in
                </TabsTrigger>
                <TabsTrigger value="register" className="flex-1">
                  Create account
                </TabsTrigger>
              </TabsList>
            </Tabs>
            <CardTitle className="sr-only">
              {mode === "login" ? "Sign in" : "Create account"}
            </CardTitle>
            <CardDescription className="pt-2">
              {mode === "login"
                ? "Welcome back to your media cloud."
                : "Usernames are 3–64 characters, passwords at least 8."}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={submit} className="grid gap-4">
              <div className="grid gap-2">
                <Label htmlFor="username">Username</Label>
                <Input
                  id="username"
                  autoComplete="username"
                  autoFocus
                  required
                  minLength={3}
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="password">Password</Label>
                <Input
                  id="password"
                  type="password"
                  autoComplete={
                    mode === "login" ? "current-password" : "new-password"
                  }
                  required
                  minLength={8}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </div>
              <Button type="submit" disabled={busy} className="mt-2 w-full">
                {busy && <Loader2Icon className="size-4 animate-spin" />}
                {mode === "login" ? "Sign in" : "Create account"}
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
