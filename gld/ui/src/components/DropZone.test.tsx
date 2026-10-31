// @vitest-environment jsdom
import { fireEvent, render } from "@testing-library/react"
import { expect, it, vi } from "vitest"
import { DIR_MIME } from "@/lib/dnd"
import { DropZone } from "./DropZone"

const dt = (data: Record<string, string>) => ({ types: Object.keys(data), getData: (key: string) => data[key] ?? "" })

it("appelle onDir avec le dossier déposé et ignore les autres glissements", () => {
  const onDir = vi.fn()
  const { container } = render(<DropZone onDir={onDir}>x</DropZone>)
  const zone = container.firstElementChild!
  fireEvent.drop(zone, { dataTransfer: dt({ "text/plain": "/tmp/a" }) })
  expect(onDir).not.toHaveBeenCalled()
  fireEvent.drop(zone, { dataTransfer: dt({ [DIR_MIME]: "/home/u/proj" }) })
  expect(onDir).toHaveBeenCalledWith("/home/u/proj")
})
