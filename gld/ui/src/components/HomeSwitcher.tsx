import { useEffect, useState } from "react"
import { Check, FolderCog, Plus, X } from "lucide-react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { api, type HomesInfo } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { tilde } from "@/lib/format"
import { useAuth } from "@/lib/auth"
import { http } from "@/lib/http"
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

const baseName = (path: string) => path.replace(/\/+$/, "").split("/").pop() || path
const FIRST_DUPLICATE_SUFFIX = 2

// Dossier de configuration Claude affiché (CLAUDE_CONFIG_DIR : compte pro, compte perso…). Choisir un autre dossier recharge la page :
// le serveur répond alors depuis un gld dédié à ce dossier (homes.go), avec ses propres sessions, mémoire et réglages.
export function HomeSwitcher() {
  const { t } = useTranslation()
  const actions = useActions()
  const auth = useAuth()
  // Multi-utilisateur : la liste reçue est filtrée par les droits, la renvoyer entière effacerait les homes invisibles. On n'envoie
  // que l'ajout, ou l'identifiant retiré ; seul un admin modifie la liste.
  const isMultiUser = auth.state === "ok"
  const canEdit = !isMultiUser || !!auth.user?.admin
  const [info, setInfo] = useState<HomesInfo | null>(null)
  useEffect(() => {
    let isStillMounted = true
    api.homes().then((homesInfo) => isStillMounted && setInfo(homesInfo), () => {})
    return () => {
      isStillMounted = false
    }
  }, [])
  if (!info) return null
  const mainLabel = info.main ? tilde(info.main, actions.home) : t("shell.home.main")
  const label = info.current || mainLabel
  const select = async (name: string) => {
    if (name === info.current) return
    try {
      await api.homeSelect(name)
      location.reload()
    } catch (error) {
      toast.error(errMsg(error))
    }
  }
  const save = async (body: { homes: { name: string; dir: string }[]; removed?: string[] }) => {
    try {
      await api.homesSave(body)
      setInfo(await api.homes())
    } catch (error) {
      toast.error(errMsg(error))
    }
  }
  const toNameAndDir = ({ name, dir }: { name: string; dir: string }) => ({ name, dir })
  const add = async () => {
    const dir = await actions.pickDir(t("shell.home.pickTitle"), info.main)
    if (!dir) return
    // Multi-utilisateur : le nom doit éviter aussi les homes invisibles de l'admin, que le serveur refuserait.
    const takenHomes = isMultiUser
      ? await http.get<{ homes: { name: string }[] }>("/admin/homes").then((adminHomes) => adminHomes.homes, () => info.homes)
      : info.homes
    const isNameTaken = (candidateName: string) => takenHomes.some((home) => home.name === candidateName)
    let name = baseName(dir)
    for (let duplicateSuffix = FIRST_DUPLICATE_SUFFIX; isNameTaken(name); duplicateSuffix++) name = `${baseName(dir)} ${duplicateSuffix}`
    if (isMultiUser) {
      await save({ homes: [{ name, dir }] })
      return
    }
    await save({ homes: [...info.homes.map(toNameAndDir), { name, dir }] })
  }
  const remove = async (homeToRemove: HomesInfo["homes"][number]) => {
    if (homeToRemove.name === info.current) await select("")
    if (isMultiUser) {
      await save({ homes: [], removed: [homeToRemove.id!] })
      return
    }
    await save({ homes: info.homes.filter((home) => home.name !== homeToRemove.name).map(toNameAndDir) })
  }
  const removable = info.homes.filter((home) => !isMultiUser || home.id)
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
          <Check className={info.current === "" ? "" : "invisible"} /> <span className="truncate">{mainLabel}</span>
        </DropdownMenuItem>
        {info.homes.map((home) => (
          <DropdownMenuItem key={home.name} onSelect={() => select(home.name)} title={home.dir}>
            <Check className={info.current === home.name ? "" : "invisible"} />
            <span className="truncate">{home.name}</span>
            <span className="ml-auto truncate text-xs text-muted-foreground">{tilde(home.dir, actions.home)}</span>
          </DropdownMenuItem>
        ))}
        {canEdit && (
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
                  {removable.map((home) => (
                    <DropdownMenuItem key={home.name} onSelect={() => remove(home)}>
                      <span className="truncate">{home.name}</span>
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
