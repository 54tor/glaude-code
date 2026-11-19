import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { api, type GitRemote, type RemoteUnpushed } from "@/lib/api"
import { startGitPushes } from "@/hooks/useGitPush"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

type Props = {
  dir: string
  branch: string
  onOpenChange: (isOpen: boolean) => void
}

// Aperçu des commits manquants sur chaque remote, puis poussée vers ceux qui sont cochés (par défaut ceux qui sont en retard).
// Monté seulement quand il est ouvert : chaque ouverture repart d'un état vierge.
export function GitPushDialog({ dir, branch, onOpenChange }: Props) {
  const { t } = useTranslation()
  const [remotes, setRemotes] = useState<GitRemote[] | null>(null)
  const [unpushed, setUnpushed] = useState<RemoteUnpushed[] | null>(null)
  const [hasPreviewFailed, setHasPreviewFailed] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [isForceTags, setIsForceTags] = useState(false)
  const [tags, setTags] = useState<string[]>([])
  const [selectedTags, setSelectedTags] = useState<Set<string>>(new Set())

  useEffect(() => {
    let isCurrent = true
    api.gitRemotes(dir).then(
      (response) => isCurrent && setRemotes(response.remotes),
      () => isCurrent && setRemotes([])
    )
    api.gitUnpushed(dir).then(
      (response) => {
        if (!isCurrent) return
        setUnpushed(response.remotes)
        const behindNames = response.remotes
          .filter((remote) => remote.total > 0)
          .map((remote) => remote.name)
        setSelected(
          new Set(
            behindNames.length > 0
              ? behindNames
              : response.remotes.map((remote) => remote.name)
          )
        )
      },
      () => isCurrent && setHasPreviewFailed(true)
    )
    api.gitTags(dir).then(
      (response) => isCurrent && setTags(response.tags),
      () => undefined
    )
    return () => {
      isCurrent = false
    }
  }, [dir])

  const unpushedOf = (name: string) =>
    unpushed?.find((remote) => remote.name === name)
  const toggle = (name: string, isChecked: boolean) => {
    const next = new Set(selected)
    if (isChecked) next.add(name)
    else next.delete(name)
    setSelected(next)
  }
  const toggleTag = (tag: string, isChecked: boolean) => {
    const next = new Set(selectedTags)
    if (isChecked) next.add(tag)
    else next.delete(tag)
    setSelectedTags(next)
  }
  const pushSelected = () => {
    const names = (remotes ?? [])
      .map((remote) => remote.name)
      .filter((name) => selected.has(name))
    onOpenChange(false)
    startGitPushes(
      dir,
      names,
      branch,
      tags.filter((tag) => selectedTags.has(tag)),
      isForceTags
    )
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="min-w-0 sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {t("conversation.gitPush.dialogTitle", { branch })}
          </DialogTitle>
          <DialogDescription>
            {t("conversation.gitPush.dialogHint")}
          </DialogDescription>
        </DialogHeader>
        <div className="flex max-h-[60vh] flex-col gap-3 overflow-y-auto">
          {remotes === null && (
            <p className="text-sm text-muted-foreground">
              {t("conversation.gitPush.reading")}
            </p>
          )}
          {remotes?.length === 0 && (
            <p className="text-sm text-muted-foreground">
              {t("conversation.gitPush.none")}
            </p>
          )}
          {hasPreviewFailed && (
            <p className="text-sm text-muted-foreground">
              {t("conversation.gitPush.previewFailed")}
            </p>
          )}
          {remotes?.map((remote) => {
            const remoteUnpushed = unpushedOf(remote.name)
            const hiddenCount = remoteUnpushed
              ? remoteUnpushed.total - remoteUnpushed.commits.length
              : 0
            return (
              <div key={remote.name} className="min-w-0 rounded-md border p-2">
                <label className="flex cursor-pointer items-center gap-2">
                  <Checkbox
                    checked={selected.has(remote.name)}
                    onCheckedChange={(isChecked) =>
                      toggle(remote.name, isChecked === true)
                    }
                  />
                  <span className="font-medium">{remote.name}</span>
                  <span className="min-w-0 truncate text-xs text-muted-foreground">
                    {remote.url}
                  </span>
                  {remoteUnpushed && (
                    <span className="ml-auto shrink-0 text-xs text-muted-foreground">
                      {remoteUnpushed.total === 0
                        ? t("conversation.gitPush.upToDate")
                        : t("conversation.gitPush.commitCount", {
                            count: remoteUnpushed.total,
                          })}
                    </span>
                  )}
                </label>
                {!unpushed && !hasPreviewFailed && (
                  <p className="mt-1 text-xs text-muted-foreground">
                    {t("conversation.gitPush.loadingCommits")}
                  </p>
                )}
                {remoteUnpushed && remoteUnpushed.commits.length > 0 && (
                  <ul className="mt-2 max-h-40 overflow-y-auto font-mono text-xs">
                    {remoteUnpushed.commits.map((commit) => (
                      <li key={commit.hash} className="flex gap-2">
                        <span className="shrink-0 text-muted-foreground">
                          {commit.hash}
                        </span>
                        <span
                          className="min-w-0 truncate"
                          title={commit.subject}
                        >
                          {commit.subject}
                        </span>
                      </li>
                    ))}
                    {hiddenCount > 0 && (
                      <li className="text-muted-foreground">
                        {t("conversation.gitPush.moreCommits", {
                          count: hiddenCount,
                        })}
                      </li>
                    )}
                  </ul>
                )}
              </div>
            )
          })}
          {tags.length > 0 && (
            <div className="rounded-md border p-2">
              <p className="mb-1 text-sm font-medium">
                {t("conversation.gitPush.tagsTitle")}
              </p>
              <ul className="max-h-40 overflow-y-auto">
                {tags.map((tag) => (
                  <li key={tag}>
                    <label className="flex cursor-pointer items-center gap-2 font-mono text-xs">
                      <Checkbox
                        checked={selectedTags.has(tag)}
                        onCheckedChange={(isChecked) =>
                          toggleTag(tag, isChecked === true)
                        }
                      />
                      {tag}
                    </label>
                  </li>
                ))}
              </ul>
              {selectedTags.size > 0 && (
                <label className="mt-2 flex cursor-pointer items-center gap-2 text-xs text-destructive">
                  <Checkbox
                    checked={isForceTags}
                    onCheckedChange={(isChecked) =>
                      setIsForceTags(isChecked === true)
                    }
                  />
                  {t("conversation.gitPush.forceTags")}
                </label>
              )}
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("conversation.gitPush.cancel")}
          </Button>
          <Button
            disabled={selected.size === 0 || !remotes}
            onClick={pushSelected}
          >
            {t("conversation.gitPush.pushSelected", { count: selected.size })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
