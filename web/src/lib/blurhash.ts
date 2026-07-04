// Minimal BlurHash decoder (https://blurha.sh) — ~1 KB, no dependency.
// Decodes into a small RGBA buffer that is scaled up by the browser, which is
// exactly how BlurHash placeholders are meant to be used.

const B83 =
  "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~"

function decode83(str: string): number {
  let value = 0
  for (const c of str) value = value * 83 + B83.indexOf(c)
  return value
}

function sRGBToLinear(value: number): number {
  const v = value / 255
  return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4)
}

function linearTosRGB(value: number): number {
  const v = Math.max(0, Math.min(1, value))
  return v <= 0.0031308
    ? Math.round(v * 12.92 * 255 + 0.5)
    : Math.round((1.055 * Math.pow(v, 1 / 2.4) - 0.055) * 255 + 0.5)
}

function signPow(v: number, exp: number): number {
  return Math.sign(v) * Math.pow(Math.abs(v), exp)
}

export function decodeBlurHash(
  hash: string,
  width: number,
  height: number
): Uint8ClampedArray<ArrayBuffer> | null {
  if (!hash || hash.length < 6) return null

  const sizeFlag = decode83(hash[0])
  const numY = Math.floor(sizeFlag / 9) + 1
  const numX = (sizeFlag % 9) + 1
  if (hash.length !== 4 + 2 * numX * numY) return null

  const quantMax = decode83(hash[1])
  const maxValue = (quantMax + 1) / 166

  const colors: Array<[number, number, number]> = new Array(numX * numY)
  const dcValue = decode83(hash.substring(2, 6))
  colors[0] = [
    sRGBToLinear(dcValue >> 16),
    sRGBToLinear((dcValue >> 8) & 255),
    sRGBToLinear(dcValue & 255),
  ]
  for (let i = 1; i < numX * numY; i++) {
    const value = decode83(hash.substring(4 + i * 2, 6 + i * 2))
    colors[i] = [
      signPow((Math.floor(value / (19 * 19)) - 9) / 9, 2) * maxValue,
      signPow((Math.floor(value / 19) % 19 - 9) / 9, 2) * maxValue,
      signPow(((value % 19) - 9) / 9, 2) * maxValue,
    ]
  }

  const pixels = new Uint8ClampedArray(width * height * 4)
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      let r = 0
      let g = 0
      let b = 0
      for (let j = 0; j < numY; j++) {
        const cy = Math.cos((Math.PI * y * j) / height)
        for (let i = 0; i < numX; i++) {
          const basis = Math.cos((Math.PI * x * i) / width) * cy
          const color = colors[i + j * numX]
          r += color[0] * basis
          g += color[1] * basis
          b += color[2] * basis
        }
      }
      const p = 4 * (x + y * width)
      pixels[p] = linearTosRGB(r)
      pixels[p + 1] = linearTosRGB(g)
      pixels[p + 2] = linearTosRGB(b)
      pixels[p + 3] = 255
    }
  }
  return pixels
}

const dataURLCache = new Map<string, string>()

/** Decodes a BlurHash into a tiny data-URL PNG, memoized per hash. */
export function blurHashToDataURL(hash: string, w = 32, h = 32): string | null {
  const cached = dataURLCache.get(hash)
  if (cached) return cached

  const pixels = decodeBlurHash(hash, w, h)
  if (!pixels) return null

  const canvas = document.createElement("canvas")
  canvas.width = w
  canvas.height = h
  const ctx = canvas.getContext("2d")
  if (!ctx) return null
  ctx.putImageData(new ImageData(pixels, w, h), 0, 0)

  const url = canvas.toDataURL()
  if (dataURLCache.size > 500) dataURLCache.clear()
  dataURLCache.set(hash, url)
  return url
}
