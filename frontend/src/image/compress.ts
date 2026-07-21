// Client-side proof-image compression.
//
// Why this exists: production logs showed a 1.25 MB upload taking 10.3 s, with
// other attempts abandoned by the client at 24 s and 188 s. The server, the
// database and Caddy were all fast — the time was spent pushing bytes over a
// phone's uplink. Shrinking the image in the browser is the fix that actually
// addresses the measurement; nothing here is a workaround for a slow backend.
//
// The module is split deliberately: everything that decides *what* to do is a
// pure function (unit-tested in Node with no DOM), and only the thin
// `compressImageFile` glue touches canvas/ImageBitmap. A payment screenshot is
// evidence, so the pipeline is conservative — it never upscales, never
// transcodes a format it cannot safely decode, and falls back to the untouched
// original whenever compression does not actually help.

/** Formats we can decode and re-encode safely in a canvas. */
export const COMPRESSIBLE_MIME_TYPES = ['image/jpeg', 'image/png', 'image/webp'] as const

/**
 * Formats that must be passed through untouched. Animated GIFs would lose every
 * frame but the first, and SVG is markup — rasterizing it silently changes what
 * the evidence is. The server applies its existing validation to these.
 */
export const PASSTHROUGH_MIME_TYPES = ['image/gif', 'image/svg+xml'] as const

export type CompressOptions = {
  /** Longest-edge ceiling in pixels. */
  maxEdge?: number
  /** Encoder quality for lossy output. */
  quality?: number
  /**
   * Size at or below which an image is left alone entirely: re-encoding a
   * screenshot that is already small only costs quality.
   */
  skipBelowBytes?: number
}

export const DEFAULT_COMPRESS_OPTIONS: Required<CompressOptions> = {
  maxEdge: 1800,
  quality: 0.8,
  skipBelowBytes: 300 * 1024,
}

export type Dimensions = { width: number; height: number }

/**
 * Scales dimensions so the longest edge is at most maxEdge, preserving aspect
 * ratio. An image already within the ceiling is returned unchanged — upscaling
 * a small screenshot would add bytes and invent detail that was never there.
 */
export function scaleToMaxEdge(width: number, height: number, maxEdge: number): Dimensions {
  if (!(width > 0) || !(height > 0) || !(maxEdge > 0)) return { width, height }
  const longest = Math.max(width, height)
  if (longest <= maxEdge) return { width, height }
  const ratio = maxEdge / longest
  return {
    width: Math.max(1, Math.round(width * ratio)),
    height: Math.max(1, Math.round(height * ratio)),
  }
}

/** EXIF orientations 5–8 transpose the image, so the drawn canvas swaps axes. */
export function orientationSwapsAxes(orientation: number): boolean {
  return orientation >= 5 && orientation <= 8
}

/** Post-rotation display size for a decoded bitmap of the given raw size. */
export function orientedSize(width: number, height: number, orientation: number): Dimensions {
  return orientationSwapsAxes(orientation) ? { width: height, height: width } : { width, height }
}

export type CanvasTransform = [number, number, number, number, number, number]

/**
 * Canvas setTransform matrix that draws a raw width×height bitmap upright, for
 * a canvas already sized with orientedSize. Matrix maps (x,y) to
 * (a*x + c*y + e, b*x + d*y + f).
 *
 * Without this, a photo taken in portrait on a phone — which is stored
 * landscape plus an orientation tag — is re-encoded sideways, and the tag is
 * lost in the process, so the sideways version becomes the permanent evidence.
 */
export function orientationTransform(orientation: number, width: number, height: number): CanvasTransform {
  switch (orientation) {
    case 2:
      return [-1, 0, 0, 1, width, 0]
    case 3:
      return [-1, 0, 0, -1, width, height]
    case 4:
      return [1, 0, 0, -1, 0, height]
    case 5:
      return [0, 1, 1, 0, 0, 0]
    case 6:
      return [0, 1, -1, 0, height, 0]
    case 7:
      return [0, -1, -1, 0, height, width]
    case 8:
      return [0, -1, 1, 0, 0, width]
    default:
      return [1, 0, 0, 1, 0, 0]
  }
}

/**
 * Reads the EXIF orientation tag out of a JPEG. Returns 1 (upright) for any
 * file that is not a JPEG, carries no EXIF, or is malformed — a corrupt tag
 * must degrade to "leave it alone", never throw.
 */
export function readJpegExifOrientation(buffer: ArrayBuffer): number {
  const view = new DataView(buffer)
  if (view.byteLength < 4 || view.getUint16(0, false) !== 0xffd8) return 1

  let offset = 2
  while (offset + 4 <= view.byteLength) {
    const marker = view.getUint16(offset, false)
    // Every marker segment starts with 0xFF; anything else means we have lost
    // sync and should stop rather than read arbitrary bytes as a tag.
    if ((marker & 0xff00) !== 0xff00) return 1
    const size = view.getUint16(offset + 2, false)
    if (size < 2) return 1

    if (marker === 0xffe1) {
      const exifStart = offset + 4
      if (exifStart + 6 > view.byteLength) return 1
      // "Exif\0\0"
      if (view.getUint32(exifStart, false) !== 0x45786966) return 1
      const tiff = exifStart + 6
      if (tiff + 8 > view.byteLength) return 1
      const endianMark = view.getUint16(tiff, false)
      if (endianMark !== 0x4949 && endianMark !== 0x4d4d) return 1
      const little = endianMark === 0x4949
      if (view.getUint16(tiff + 2, little) !== 0x002a) return 1
      const ifdOffset = view.getUint32(tiff + 4, little)
      const ifd = tiff + ifdOffset
      if (ifd + 2 > view.byteLength) return 1
      const entries = view.getUint16(ifd, little)
      for (let index = 0; index < entries; index += 1) {
        const entry = ifd + 2 + index * 12
        if (entry + 12 > view.byteLength) return 1
        if (view.getUint16(entry, little) === 0x0112) {
          const value = view.getUint16(entry + 8, little)
          return value >= 1 && value <= 8 ? value : 1
        }
      }
      return 1
    }

    // 0xFFDA is start-of-scan: EXIF never appears after the pixel data.
    if (marker === 0xffda) return 1
    offset += 2 + size
  }
  return 1
}

/**
 * Picks the output container. PNG is kept only when the source is PNG and the
 * result actually came out smaller (flat UI screenshots often do); otherwise
 * lossy wins by a wide margin on photographed payment screens. WebP is used
 * only when the browser proved it can encode it, since a silent failure would
 * hand us a PNG blob mislabeled as WebP.
 */
export function chooseOutputMime(sourceMime: string, webpEncodable: boolean): string {
  if (sourceMime === 'image/webp' && webpEncodable) return 'image/webp'
  if (webpEncodable) return 'image/webp'
  return 'image/jpeg'
}

/** Compression is only worth keeping if it actually saved a meaningful amount. */
export function isWorthKeeping(originalBytes: number, candidateBytes: number): boolean {
  if (!(candidateBytes > 0)) return false
  return candidateBytes < originalBytes * 0.9
}

export type CompressionDecision =
  | { action: 'passthrough'; reason: 'unsupported-format' | 'already-small' }
  | { action: 'compress' }

/**
 * Decides whether a file should be touched at all, before any decoding work.
 */
export function decideCompression(
  mimeType: string,
  byteSize: number,
  options: Required<CompressOptions>,
): CompressionDecision {
  if (!(COMPRESSIBLE_MIME_TYPES as readonly string[]).includes(mimeType)) {
    return { action: 'passthrough', reason: 'unsupported-format' }
  }
  if (byteSize <= options.skipBelowBytes) {
    return { action: 'passthrough', reason: 'already-small' }
  }
  return { action: 'compress' }
}

export type CompressionResult = {
  /** The file to upload — the compressed one, or the original when skipped. */
  file: File
  originalBytes: number
  outputBytes: number
  width: number
  height: number
  /** False when the original was kept as-is. */
  compressed: boolean
  reason?: 'unsupported-format' | 'already-small' | 'not-smaller' | 'decode-failed'
}

function canEncodeWebP(): boolean {
  try {
    const probe = document.createElement('canvas')
    probe.width = 1
    probe.height = 1
    return probe.toDataURL('image/webp').startsWith('data:image/webp')
  } catch {
    return false
  }
}

function canvasToBlob(canvas: HTMLCanvasElement, mime: string, quality: number): Promise<Blob | null> {
  return new Promise((resolve) => canvas.toBlob(resolve, mime, quality))
}

/**
 * Compresses a proof image in the browser. Never rejects: any failure to
 * decode or encode falls back to uploading the untouched original, because a
 * slightly slow upload is always better than a blocked payment submission.
 */
export async function compressImageFile(file: File, options: CompressOptions = {}): Promise<CompressionResult> {
  const settings = { ...DEFAULT_COMPRESS_OPTIONS, ...options }
  const originalBytes = file.size
  const keepOriginal = (reason: CompressionResult['reason'], width = 0, height = 0): CompressionResult => ({
    file,
    originalBytes,
    outputBytes: originalBytes,
    width,
    height,
    compressed: false,
    reason,
  })

  const decision = decideCompression(file.type, originalBytes, settings)
  if (decision.action === 'passthrough') return keepOriginal(decision.reason)

  try {
    const buffer = await file.arrayBuffer()
    const orientation = file.type === 'image/jpeg' ? readJpegExifOrientation(buffer) : 1
    // 'from-image' lets the browser apply EXIF itself; when it does, we must
    // NOT rotate again, or a portrait photo lands upside down.
    let bitmap: ImageBitmap
    let orientationApplied = false
    try {
      bitmap = await createImageBitmap(new Blob([buffer], { type: file.type }), { imageOrientation: 'from-image' })
      orientationApplied = true
    } catch {
      bitmap = await createImageBitmap(new Blob([buffer], { type: file.type }))
    }

    const raw = { width: bitmap.width, height: bitmap.height }
    const effective = orientationApplied ? raw : orientedSize(raw.width, raw.height, orientation)
    const target = scaleToMaxEdge(effective.width, effective.height, settings.maxEdge)

    const canvas = document.createElement('canvas')
    canvas.width = target.width
    canvas.height = target.height
    const context = canvas.getContext('2d')
    if (!context) {
      bitmap.close()
      return keepOriginal('decode-failed')
    }

    if (orientationApplied) {
      context.drawImage(bitmap, 0, 0, target.width, target.height)
    } else {
      const scaleX = target.width / effective.width
      const scaleY = target.height / effective.height
      const [a, b, c, d, e, f] = orientationTransform(orientation, raw.width, raw.height)
      context.setTransform(a * scaleX, b * scaleX, c * scaleY, d * scaleY, e * scaleX, f * scaleY)
      context.drawImage(bitmap, 0, 0)
      context.setTransform(1, 0, 0, 1, 0, 0)
    }
    bitmap.close()

    const outputMime = chooseOutputMime(file.type, canEncodeWebP())
    const blob = await canvasToBlob(canvas, outputMime, settings.quality)
    if (!blob) return keepOriginal('decode-failed', target.width, target.height)

    if (!isWorthKeeping(originalBytes, blob.size)) {
      return keepOriginal('not-smaller', target.width, target.height)
    }

    const extension = outputMime === 'image/webp' ? 'webp' : 'jpg'
    const compressed = new File([blob], `proof.${extension}`, { type: outputMime, lastModified: Date.now() })
    return {
      file: compressed,
      originalBytes,
      outputBytes: compressed.size,
      width: target.width,
      height: target.height,
      compressed: true,
    }
  } catch {
    return keepOriginal('decode-failed')
  }
}
