import { Component, type ErrorInfo, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { retryFailedImports } from "@/lib/retryLazy"

const CHUNK = /dynamically imported module|importing a module|module script|chunk|Failed to fetch/i

function Fallback({ error, onRetry }: { error: Error; onRetry(): void }) {
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
      <div className="flex gap-2">
        <Button variant="outline" onClick={onRetry}>
          {t("shell.routeError.retry")}
        </Button>
        <Button onClick={() => location.reload()}>{t("shell.routeError.reload")}</Button>
      </div>
    </div>
  )
}

// À clé par route : changer de route remonte la frontière et efface l'erreur. « Réessayer » l'efface sans quitter la route
// (erreur passagère, chunk revenu) ; les routes passent par retryLazy pour qu'un import rejeté soit relancé.
export class RouteErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("route en erreur", error, info.componentStack)
  }
  render() {
    return this.state.error ? <Fallback error={this.state.error} onRetry={() => (retryFailedImports(), this.setState({ error: null }))} /> : this.props.children
  }
}
