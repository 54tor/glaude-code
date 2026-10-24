import { Component, type ErrorInfo, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"

const CHUNK = /dynamically imported module|importing a module|module script|chunk|Failed to fetch/i

function Fallback({ error }: { error: Error }) {
  const { t } = useTranslation()
  return (
    <div role="alert" className="m-6 flex flex-col items-start gap-2">
      <p className="font-medium">{t("shell.routeError.title")}</p>
      {CHUNK.test(error.message) && (
        <>
          <p className="text-sm text-muted-foreground">{t("shell.routeError.hint")}</p>
          <p className="text-xs text-muted-foreground">{error.message}</p>
        </>
      )}
      <Button onClick={() => location.reload()}>{t("shell.routeError.reload")}</Button>
    </div>
  )
}

// À clé par route : changer de route remonte la frontière et efface l'erreur.
export class RouteErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("route en erreur", error, info.componentStack)
  }
  render() {
    return this.state.error ? <Fallback error={this.state.error} /> : this.props.children
  }
}
