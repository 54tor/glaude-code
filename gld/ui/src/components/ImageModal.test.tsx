// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { ImageModal } from "./ImageModal"

afterEach(cleanup)

describe("ImageModal", () => {
  it("ouvre l'image dans une modale au clic, sans lien vers un nouvel onglet", () => {
    render(
      <ImageModal src="/img.png" name="capture.png">
        <img src="/img.png" alt="miniature" />
      </ImageModal>
    )
    expect(document.querySelector("a[target=_blank]")).toBeNull()
    expect(screen.queryByRole("dialog")).toBeNull()

    fireEvent.click(screen.getByRole("button"))

    expect(screen.getByRole("dialog")).toBeTruthy()
    expect(screen.getByAltText("capture.png")).toBeTruthy()
  })
})
