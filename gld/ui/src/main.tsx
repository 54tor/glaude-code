import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import "./index.css"
import "./i18n"
import App from "./App.tsx"
import { GuestApp } from "@/components/GuestApp.tsx"
import { ThemeProvider } from "@/components/theme-provider.tsx"
import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"
import { EDGE } from "@/lib/swipe"

// Hauteur réellement visible (clavier virtuel ouvert ou non) : les fenêtres plein écran s'y calent, et on replie l'en-tête quand
// le clavier mange la moitié de l'écran.
const vv = window.visualViewport
if (vv) {
  const fit = () => {
    const r = document.documentElement
    r.style.setProperty("--vvh", `${vv.height}px`)
    r.style.setProperty("--vvt", `${vv.offsetTop}px`)
    r.toggleAttribute("data-kb", vv.height < window.screen.height * 0.6 && window.matchMedia("(max-width: 767px)").matches)
  }
  vv.addEventListener("resize", fit)
  vv.addEventListener("scroll", fit)
  fit()
}

// PWA iPhone : un balayage depuis le bord gauche déclenche le retour arrière de WebKit au lieu d'ouvrir le tiroir. Annuler le
// touchstart dans la bande du bord l'en empêche ; les contrôles y gardent leur toucher (le bouton du menu est tout près du bord).
document.addEventListener(
  "touchstart",
  (e) => {
    if (e.touches.length === 1 && e.touches[0].clientX <= EDGE && !(e.target as Element).closest?.("button,a,input,textarea,select,label,[role=button]")) e.preventDefault()
  },
  { passive: false }
)

// Service worker : cache des ressources statiques et clic sur les notifications (contexte sécurisé : HTTPS ou localhost).
if (import.meta.env.PROD && "serviceWorker" in navigator) navigator.serviceWorker.register("/sw.js").catch(() => {})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <TooltipProvider>
        {location.pathname === "/guest" || location.pathname.startsWith("/guest/") ? <GuestApp /> : <App />}
        <Toaster position="bottom-right" mobileOffset={{ bottom: "calc(16px + env(safe-area-inset-bottom))" }} richColors closeButton />
      </TooltipProvider>
    </ThemeProvider>
  </StrictMode>
)
