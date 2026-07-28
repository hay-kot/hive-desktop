// Client-side guard rails for an image picker — shared by the profile-avatar
// picker and the webhook source's feed-mark picker. The Go core does the
// authoritative work (decode, normalize, re-encode), so this only rejects the
// obvious before shipping bytes over the Wails bridge and hands the file over
// as base64.

/** Formats the picker accepts. The Go side decodes the same set. */
export const imageUploadAccept = 'image/png,image/jpeg,image/gif,image/webp'

/** Upper bound on a picked file, mirroring the core's MaxInputBytes. */
export const imageMaxBytes = 8 * 1024 * 1024

export class ImageUploadError extends Error {}

/**
 * Reads a picked image file into the bare base64 string the backend setters
 * expect. Rejects an empty or oversized file with a message fit to show the
 * user; anything the format can't handle is left for the backend to reject with
 * its own message.
 */
export async function fileToImageBase64(file: File): Promise<string> {
  if (file.size === 0) throw new ImageUploadError('That file is empty.')
  if (file.size > imageMaxBytes) throw new ImageUploadError('That image is too large. Choose a file under 8 MB.')

  const dataUrl = await readAsDataURL(file)
  const comma = dataUrl.indexOf(',')
  if (comma < 0) throw new ImageUploadError('That image could not be read.')
  return dataUrl.slice(comma + 1)
}

function readAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(typeof reader.result === 'string' ? reader.result : '')
    reader.onerror = () => reject(new ImageUploadError('That image could not be read.'))
    reader.readAsDataURL(file)
  })
}
