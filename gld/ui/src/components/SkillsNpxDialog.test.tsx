// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { SkillsNpxDialog } from "./SkillsNpxDialog"

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const hit = {
  ref: "anthropics/skills@pdf",
  source: "anthropics/skills",
  skill: "pdf",
  installs: "217.5K",
  url: "https://skills.sh/anthropics/skills/pdf",
}

async function searched() {
  vi.spyOn(api, "skillsFind").mockResolvedValue({ hits: [hit] })
  const post = vi
    .spyOn(api, "post")
    .mockResolvedValue({ ok: true, out: "fait" } as Awaited<
      ReturnType<typeof api.post>
    >)
  const onChange = vi.fn()
  render(
    <SkillsNpxDialog
      open
      onClose={() => {}}
      projects={[{ dir: "-p", project: "/p" }]}
      home="/h"
      onChange={onChange}
    />
  )
  fireEvent.change(screen.getByPlaceholderText("Rechercher un skill…"), {
    target: { value: "pdf" },
  })
  fireEvent.click(screen.getByRole("button", { name: "Rechercher" }))
  await screen.findByText("anthropics/skills@pdf")
  return { post, onChange }
}

describe("SkillsNpxDialog", () => {
  it("affiche les résultats et n'installe rien avant la confirmation", async () => {
    const { post } = await searched()
    fireEvent.click(screen.getByRole("button", { name: "Installer" }))
    expect(post).not.toHaveBeenCalled()
    expect(
      await screen.findByText(
        /npx --yes skills@1\.7\.4 add anthropics\/skills -s pdf -a claude-code -y -g/
      )
    ).toBeTruthy()
    expect(
      screen.getByText(/n'installez que des sources de confiance/)
    ).toBeTruthy()
  })

  it("installe en global après confirmation, puis prévient la liste", async () => {
    const { post, onChange } = await searched()
    fireEvent.click(screen.getByRole("button", { name: "Installer" }))
    await screen.findByText(/Commande/)
    fireEvent.click(
      screen.getByRole("button", { name: "Confirmer l'installation" })
    )
    await waitFor(() => expect(onChange).toHaveBeenCalled())
    expect(post).toHaveBeenCalledWith("/skill-npx", {
      Action: "add",
      Source: "anthropics/skills",
      Skill: "pdf",
      Scope: "user",
      Project: "",
    })
  })
})
