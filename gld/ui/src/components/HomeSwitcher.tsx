import { useEffect, useState } from "react"
import { Check, FolderCog, Plus, X } from "lucide-react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { api, type HomesInfo } from "@/lib/api"
import { tilde } from "@/lib/format"
import { useAuth } from "@/lib/auth"
import { Button } from "@/components/ui/button"
import { Tip } from "@/components/ui/tooltip"
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
  const auth = useAuth()
  // Multi-utilisateur : la liste reçue est filtrée par les droits, la renvoyer entière effacerait les homes invisibles. On n'envoie
  // que l'ajout, ou l'identifiant retiré ; seul un admin modifie la liste.
  const multi = auth.state === "ok"
  const edit = !multi || !!auth.user?.admin
  const [info, setInfo] = useState<HomesInfo | null>(null)
  useEffect(() => {
    let live = true
    api.homes().then((r) => live && setInfo(r), () => {})
    return () => {
      live = false
    }
  }, [])
  if (!info) return null
  const main = info.main ? tilde(info.main, a.home) : t("shell.home.main")
  const label = info.current || main
  const select = async (name: string) => {
    if (name === info.current) return
    const r = await api.post("/homes-select", { name })
    if (r.ok) location.reload()
    else toast.error(r.out)
  }
  const save = async (body: { homes: { name: string; dir: string }[]; removed?: string[] }) => {
    const r = await api.post("/homes-save", body)
    if (!r.ok) return void toast.error(r.out)
    setInfo(await api.homes())
  }
  const add = async () => {
    const dir = await a.pickDir(t("shell.home.pickTitle"), info.main)
    if (!dir) return
    let name = base(dir)
    for (let i = 2; info.homes.some((h) => h.name === name); i++) name = `${base(dir)} ${i}`
    await save({ homes: multi ? [{ name, dir }] : [...info.homes.map(({ name, dir }) => ({ name, dir })), { name, dir }] })
  }
  const remove = async (h: HomesInfo["homes"][number]) => {
    if (h.name === info.current) await select("")
    await save(multi ? { homes: [], removed: [h.id!] } : { homes: info.homes.filter((x) => x.name !== h.name).map(({ name, dir }) => ({ name, dir })) })
  }
  const removable = info.homes.filter((h) => !multi || h.id)
  return (
    <DropdownMenu>
      <Tip label={t("shell.home.titleWith", { label })}>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon" className="size-9 text-muted-foreground" aria-label={t("shell.home.label")}>
            <FolderCog className="size-6" />
          </Button>
        </DropdownMenuTrigger>
      </Tip>
      <DropdownMenuContent align="end" className="w-72">
        <DropdownMenuLabel>{t("shell.home.label")}</DropdownMenuLabel>
        <DropdownMenuItem onSelect={() => select("")}>
          <Check className={info.current === "" ? "" : "invisible"} /> <span className="truncate">{main}</span>
        </DropdownMenuItem>
        {info.homes.map((h) => (
          <DropdownMenuItem key={h.name} onSelect={() => select(h.name)} title={h.dir}>
            <Check className={info.current === h.name ? "" : "invisible"} />
            <span className="truncate">{h.name}</span>
            <span className="ml-auto truncate text-xs text-muted-foreground">{tilde(h.dir, a.home)}</span>
          </DropdownMenuItem>
        ))}
        {edit && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={add}>
              <Plus /> {t("shell.home.add")}
            </DropdownMenuItem>
            {removable.length > 0 && (
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <X /> {t("shell.home.remove")}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="w-56">
                  {removable.map((h) => (
                    <DropdownMenuItem key={h.name} onSelect={() => remove(h)}>
                      <span className="truncate">{h.name}</span>
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            )}
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
