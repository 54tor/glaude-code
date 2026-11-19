"use client"

import { useEffect, useState } from "react"
import { useTheme } from "next-themes"
import { Toaster as Sonner, type ToasterProps } from "sonner"
import {
  CircleCheckIcon,
  InfoIcon,
  TriangleAlertIcon,
  OctagonXIcon,
  FanIcon,
} from "lucide-react"
import { useTranslation } from "react-i18next"

// Les dialogues (tiroir, feuille du bas) sont portés en enfant direct de body : l'observer n'écoute pas le sous-arbre.
function useDialogOpen() {
  const [dialogOpen, setDialogOpen] = useState(false)
  useEffect(() => {
    const sync = () =>
      setDialogOpen(document.querySelector('[role="dialog"]') !== null)
    sync()
    const observer = new MutationObserver(sync)
    observer.observe(document.body, { childList: true })
    return () => observer.disconnect()
  }, [])
  return dialogOpen
}

const Toaster = ({ ...props }: ToasterProps) => {
  const { theme = "system" } = useTheme()
  const dialogOpen = useDialogOpen()
  const { t } = useTranslation()

  return (
    <Sonner
      theme={theme as ToasterProps["theme"]}
      className="toaster group pointer-events-auto"
      icons={{
        success: <CircleCheckIcon className="size-4" />,
        info: <InfoIcon className="size-4" />,
        warning: <TriangleAlertIcon className="size-4" />,
        error: <OctagonXIcon className="size-4" />,
        loading: <FanIcon className="size-4 animate-spin" />,
      }}
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
          "--border-radius": "var(--radius)",
        } as React.CSSProperties
      }
      containerAriaLabel={t("shell.toast.notifications")}
      toastOptions={{
        classNames: {
          toast: "cn-toast",
        },
      }}
      {...props}
      position={dialogOpen ? "top-center" : props.position}
      mobileOffset={
        dialogOpen
          ? { top: "calc(16px + env(safe-area-inset-top))" }
          : props.mobileOffset
      }
    />
  )
}

export { Toaster }
