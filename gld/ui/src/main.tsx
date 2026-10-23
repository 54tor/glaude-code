import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import "./index.css"
import "./i18n"
import App from "./App.tsx"
import { ThemeProvider } from "@/components/theme-provider.tsx"
import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"

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

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <TooltipProvider>
        <App />
        <Toaster position="bottom-right" richColors closeButton />
      </TooltipProvider>
    </ThemeProvider>
  </StrictMode>
)
