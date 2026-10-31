import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import "./index.css"
import "./i18n"
import App from "./App.tsx"
import { GuestApp } from "@/components/GuestApp.tsx"
import { ThemeProvider } from "@/components/theme-provider.tsx"
import { TrustDialog } from "@/components/TrustDialog"
import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"
import { EDGE } from "@/lib/swipe"

const KEYBOARD_OPEN_SCREEN_RATIO = 0.6
const MOBILE_MEDIA_QUERY = "(max-width: 767px)"
const TOUCH_CONTROLS_SELECTOR = "button,a,input,textarea,select,label,[role=button]"

// Hauteur réellement visible (clavier virtuel ouvert ou non) : les fenêtres plein écran s'y calent, et on replie l'en-tête quand
// le clavier mange la moitié de l'écran.
const visualViewport = window.visualViewport
if (visualViewport) {
  const fitToVisualViewport = () => {
    const rootElement = document.documentElement
    rootElement.style.setProperty("--vvh", `${visualViewport.height}px`)
    rootElement.style.setProperty("--vvt", `${visualViewport.offsetTop}px`)
    const isKeyboardCoveringScreen = visualViewport.height < window.screen.height * KEYBOARD_OPEN_SCREEN_RATIO
    const isMobileWidth = window.matchMedia(MOBILE_MEDIA_QUERY).matches
    rootElement.toggleAttribute("data-kb", isKeyboardCoveringScreen && isMobileWidth)
  }
  visualViewport.addEventListener("resize", fitToVisualViewport)
  visualViewport.addEventListener("scroll", fitToVisualViewport)
  fitToVisualViewport()
}

// PWA iPhone : un balayage depuis le bord gauche déclenche le retour arrière de WebKit au lieu d'ouvrir le tiroir. Annuler le
// touchstart dans la bande du bord l'en empêche ; les contrôles y gardent leur toucher (le bouton du menu est tout près du bord).
document.addEventListener(
  "touchstart",
  (touchEvent) => {
    const isSingleTouch = touchEvent.touches.length === 1
    if (!isSingleTouch) return
    const isInLeftEdge = touchEvent.touches[0].clientX <= EDGE
    if (!isInLeftEdge) return
    const isOnControl = (touchEvent.target as Element).closest?.(TOUCH_CONTROLS_SELECTOR)
    if (isOnControl) return
    touchEvent.preventDefault()
  },
  { passive: false }
)

// Service worker : cache des ressources statiques et clic sur les notifications (contexte sécurisé : HTTPS ou localhost).
if (import.meta.env.PROD && "serviceWorker" in navigator) navigator.serviceWorker.register("/sw.js").catch(() => {})

const isGuestPath = location.pathname === "/guest" || location.pathname.startsWith("/guest/")

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <TooltipProvider>
        {isGuestPath ? <GuestApp /> : <App />}
        <TrustDialog />
        <Toaster position="bottom-right" mobileOffset={{ bottom: "calc(16px + env(safe-area-inset-bottom))" }} richColors closeButton />
      </TooltipProvider>
    </ThemeProvider>
  </StrictMode>
)
