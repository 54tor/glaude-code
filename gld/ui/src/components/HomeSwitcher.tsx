import { useEffect, useState } from "react"
import { Check, FolderCog, Plus, X } from "lucide-react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { api, type HomesInfo } from "@/lib/api"
import { tilde } from "@/lib/format"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useActions } from "./actions"

const base = (p: string) => p.replace(/\/+$/, "").split("/").pop() || p

// Dossier de configuration Claude affiché (CLAUDE_CONFIG_DIR : compte pro, compte perso…). Choisir un autre dossier recharge la page :
// le serveur répond alors depuis un gld dédié à ce dossier (homes.go), avec ses propres sessions, mémoire et réglages.
export function HomeSwitcher() {
  const { t } = useTranslation()
  const a = useActions()
  const [info, setInfo] = useState<HomesInfo | null>(null)
  useEffect(() => {
    let live = true
    api.homes().then((r) => live && setInfo(r), () => {})
    return () => {
      live = false
    }
  }, [])
  if (!info) return null
  const label = info.current || tilde(info.main, a.home)
  const select = async (name: string) => {
    if (name === info.current) return
    const r = await api.post("/homes-select", { name })
    if (r.ok) location.reload()
    else toast.error(r.out)
  }
  const save = async (homes: { name: string; dir: string }[]) => {
    const r = await api.post("/homes-save", { homes })
    if (!r.ok) return void toast.error(r.out)
    setInfo(await api.homes())
  }
  const add = async () => {
    const dir = await a.pickDir(t("shell.home.pickTitle"), info.main)
    if (!dir) return
    let name = base(dir)
    for (let i = 2; info.homes.some((h) => h.name === name); i++) name = `${base(dir)} ${i}`
    await save([...info.homes.map(({ name, dir }) => ({ name, dir })), { name, dir }])
  }
  const remove = async (name: string) => {
    if (name === info.current) await select("")
    await save(info.homes.filter((h) => h.name !== name).map(({ name, dir }) => ({ name, dir })))
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="size-9 text-muted-foreground" aria-label={t("shell.home.label")} title={t("shell.home.titleWith", { label })}>
          <FolderCog className="size-6" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-72">
        <DropdownMenuLabel>{t("shell.home.label")}</DropdownMenuLabel>
        <DropdownMenuItem onSelect={() => select("")}>
          <Check className={info.current === "" ? "" : "invisible"} /> <span className="truncate">{tilde(info.main, a.home)}</span>
        </DropdownMenuItem>
        {info.homes.map((h) => (
          <DropdownMenuItem key={h.name} onSelect={() => select(h.name)} title={h.dir}>
            <Check className={info.current === h.name ? "" : "invisible"} />
            <span className="truncate">{h.name}</span>
            <span className="ml-auto truncate text-xs text-muted-foreground">{tilde(h.dir, a.home)}</span>
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={add}>
          <Plus /> {t("shell.home.add")}
        </DropdownMenuItem>
        {info.homes.length > 0 && (
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <X /> {t("shell.home.remove")}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="w-56">
              {info.homes.map((h) => (
                <DropdownMenuItem key={h.name} onSelect={() => remove(h.name)}>
                  <span className="truncate">{h.name}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
