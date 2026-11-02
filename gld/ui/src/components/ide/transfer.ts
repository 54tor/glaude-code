import { authFetch } from "@/lib/auth"
import { netError, qs, readResponse, v1 } from "@/lib/apiError"

export const MAX_UPLOAD_BYTES = 64 << 20

export async function uploadFile(
  dir: string,
  path: string,
  file: File
): Promise<void> {
  try {
    const response = await authFetch(
      v1(`/fs/entries/upload${qs({ dir, path })}`),
      {
        method: "POST",
        headers: { "Content-Type": "application/octet-stream" },
        body: file,
      }
    )
    await readResponse<unknown>(response)
  } catch (uploadError) {
    throw netError(uploadError)
  }
}

export async function downloadFile(
  dir: string,
  path: string,
  name: string
): Promise<void> {
  let blob: Blob
  try {
    const response = await authFetch(v1(`/fs/entries/raw${qs({ dir, path })}`))
    if (!response.ok) await readResponse<unknown>(response)
    blob = await response.blob()
  } catch (downloadError) {
    throw netError(downloadError)
  }
  const objectUrl = URL.createObjectURL(blob)
  const link = document.createElement("a")
  link.href = objectUrl
  link.download = name
  link.click()
  setTimeout(() => URL.revokeObjectURL(objectUrl), 0)
}
