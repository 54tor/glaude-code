import { createElement, lazy, type ComponentProps, type ComponentType } from "react"

// React garde pour toujours un import paresseux rejeté. Un lazy neuf n'est créé qu'à la demande (« Réessayer » de
// RouteErrorBoundary) : le créer dès l'échec ferait relancer l'import par React à chaque rendu, en boucle.
const failed = new Set<() => void>()
export const retryFailedImports = () => (failed.forEach((f) => f()), failed.clear())

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- même contrainte que lazy() de React
export function retryLazy<T extends ComponentType<any>>(load: () => Promise<{ default: T }>): T {
  const make = () =>
    lazy(() =>
      load().catch((e) => {
        failed.add(() => (C = make()))
        throw e
      })
    )
  let C = make()
  return ((p: ComponentProps<T>) => createElement(C as ComponentType<ComponentProps<T>>, p)) as unknown as T
}
