import { createElement, lazy, type ComponentProps, type ComponentType } from "react"

// React garde pour toujours un import paresseux rejeté. Un lazy neuf n'est créé qu'à la demande (« Réessayer » de
// RouteErrorBoundary) : le créer dès l'échec ferait relancer l'import par React à chaque rendu, en boucle.
const pendingRetries = new Set<() => void>()

export const retryFailedImports = () => {
  pendingRetries.forEach((recreateLazyComponent) => recreateLazyComponent())
  pendingRetries.clear()
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- même contrainte que lazy() de React
export function retryLazy<T extends ComponentType<any>>(load: () => Promise<{ default: T }>): T {
  const createLazyComponent = () =>
    lazy(() =>
      load().catch((loadError) => {
        pendingRetries.add(() => {
          LazyComponent = createLazyComponent()
        })
        throw loadError
      })
    )
  let LazyComponent = createLazyComponent()
  const RetryingComponent = (props: ComponentProps<T>) => createElement(LazyComponent as ComponentType<ComponentProps<T>>, props)
  return RetryingComponent as unknown as T
}
