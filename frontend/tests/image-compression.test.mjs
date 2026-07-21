import assert from 'node:assert/strict'
import test from 'node:test'

import {
  DEFAULT_COMPRESS_OPTIONS,
  chooseOutputMime,
  decideCompression,
  isWorthKeeping,
  orientationSwapsAxes,
  orientationTransform,
  orientedSize,
  readJpegExifOrientation,
  scaleToMaxEdge,
} from '../src/image/compress.ts'

// 生产日志：1.25 MB 上传耗时 10.3 秒，另有 24 秒和 188 秒被客户端中断。
// 服务器、数据库、Caddy 均为毫秒级，时间全花在手机上行带宽上，
// 因此压缩在浏览器端完成，这里覆盖其纯逻辑部分。

test('大图按长边缩放，宽高比保持不变', () => {
  const result = scaleToMaxEdge(4032, 3024, 1800)
  assert.equal(result.width, 1800)
  assert.equal(result.height, 1350)
  assert.ok(Math.abs(result.width / result.height - 4032 / 3024) < 0.01)
})

test('竖图按较长的高缩放', () => {
  const result = scaleToMaxEdge(1080, 2400, 1800)
  assert.equal(result.height, 1800)
  assert.equal(result.width, 810)
})

test('小图绝不被放大', () => {
  const result = scaleToMaxEdge(800, 600, 1800)
  assert.deepEqual(result, { width: 800, height: 600 })
})

test('恰好等于上限的图不缩放', () => {
  assert.deepEqual(scaleToMaxEdge(1800, 900, 1800), { width: 1800, height: 900 })
})

test('非法尺寸原样返回，不抛异常', () => {
  assert.deepEqual(scaleToMaxEdge(0, 0, 1800), { width: 0, height: 0 })
})

// ---- EXIF 方向 ----

test('EXIF 5–8 需要交换宽高，1–4 不需要', () => {
  for (const orientation of [1, 2, 3, 4]) assert.equal(orientationSwapsAxes(orientation), false)
  for (const orientation of [5, 6, 7, 8]) assert.equal(orientationSwapsAxes(orientation), true)
})

test('横向存储的竖拍照片，方向 6 下显示尺寸被转正', () => {
  assert.deepEqual(orientedSize(4032, 3024, 6), { width: 3024, height: 4032 })
  assert.deepEqual(orientedSize(4032, 3024, 1), { width: 4032, height: 3024 })
})

// 矩阵 [a,b,c,d,e,f] 把 (x,y) 映射到 (a*x + c*y + e, b*x + d*y + f)。
const applyTransform = ([a, b, c, d, e, f], x, y) => [a * x + c * y + e, b * x + d * y + f]

test('方向 1 是恒等变换', () => {
  assert.deepEqual(orientationTransform(1, 100, 50), [1, 0, 0, 1, 0, 0])
})

test('方向 6（顺时针 90°）把源图左上角送到画布右上角', () => {
  const width = 100
  const height = 50
  const matrix = orientationTransform(6, width, height)
  // 画布为 height×width。
  assert.deepEqual(applyTransform(matrix, 0, 0), [height, 0])
  assert.deepEqual(applyTransform(matrix, width, 0), [height, width])
  assert.deepEqual(applyTransform(matrix, 0, height), [0, 0])
})

test('方向 3（180°）把左上角送到右下角', () => {
  const matrix = orientationTransform(3, 100, 50)
  assert.deepEqual(applyTransform(matrix, 0, 0), [100, 50])
  assert.deepEqual(applyTransform(matrix, 100, 50), [0, 0])
})

test('方向 8（逆时针 90°）把左上角送到左下角', () => {
  const width = 100
  const height = 50
  const matrix = orientationTransform(8, width, height)
  assert.deepEqual(applyTransform(matrix, 0, 0), [0, width])
  assert.deepEqual(applyTransform(matrix, width, 0), [0, 0])
})

test('每个方向的变换都把源图四角映射到画布范围内', () => {
  const width = 100
  const height = 50
  for (let orientation = 1; orientation <= 8; orientation += 1) {
    const canvas = orientedSize(width, height, orientation)
    const matrix = orientationTransform(orientation, width, height)
    for (const [x, y] of [[0, 0], [width, 0], [0, height], [width, height]]) {
      const [mappedX, mappedY] = applyTransform(matrix, x, y)
      assert.ok(
        mappedX >= 0 && mappedX <= canvas.width && mappedY >= 0 && mappedY <= canvas.height,
        `方向 ${orientation}：角点 (${x},${y}) 映射到画布外 (${mappedX},${mappedY})`,
      )
    }
  }
})

// ---- EXIF 解析 ----

/** 构造一个带 EXIF Orientation 标签的最小 JPEG 头。 */
function jpegWithOrientation(orientation, { littleEndian = true } = {}) {
  const exifPayload = 6 + 8 + 2 + 12 + 4 // "Exif\0\0" + TIFF 头 + 条目数 + 一条条目 + 下一 IFD 偏移
  const bytes = new Uint8Array(2 + 2 + 2 + exifPayload)
  const view = new DataView(bytes.buffer)
  let offset = 0
  view.setUint16(offset, 0xffd8, false); offset += 2 // SOI
  view.setUint16(offset, 0xffe1, false); offset += 2 // APP1
  view.setUint16(offset, 2 + exifPayload, false); offset += 2 // 段长度
  view.setUint32(offset, 0x45786966, false); offset += 4 // "Exif"
  view.setUint16(offset, 0x0000, false); offset += 2
  const tiff = offset
  view.setUint16(offset, littleEndian ? 0x4949 : 0x4d4d, false); offset += 2
  view.setUint16(offset, 0x002a, littleEndian); offset += 2
  view.setUint32(offset, 8, littleEndian); offset += 4 // IFD0 偏移
  view.setUint16(tiff + 8, 1, littleEndian) // 条目数
  const entry = tiff + 10
  view.setUint16(entry, 0x0112, littleEndian) // Orientation 标签
  view.setUint16(entry + 2, 3, littleEndian) // SHORT
  view.setUint32(entry + 4, 1, littleEndian)
  view.setUint16(entry + 8, orientation, littleEndian)
  return bytes.buffer
}

test('读出小端 JPEG 的 EXIF 方向', () => {
  for (const orientation of [1, 3, 6, 8]) {
    assert.equal(readJpegExifOrientation(jpegWithOrientation(orientation)), orientation)
  }
})

test('读出大端 JPEG 的 EXIF 方向', () => {
  assert.equal(readJpegExifOrientation(jpegWithOrientation(6, { littleEndian: false })), 6)
})

test('非 JPEG、空数据和损坏数据一律降级为方向 1，不抛异常', () => {
  assert.equal(readJpegExifOrientation(new ArrayBuffer(0)), 1)
  assert.equal(readJpegExifOrientation(new Uint8Array([0x89, 0x50, 0x4e, 0x47]).buffer), 1)
  assert.equal(readJpegExifOrientation(new Uint8Array([0xff, 0xd8, 0xff, 0xe1, 0x00]).buffer), 1)
})

test('超出 1–8 的方向值被当作方向 1', () => {
  assert.equal(readJpegExifOrientation(jpegWithOrientation(99)), 1)
})

// ---- 压缩决策 ----

test('GIF 与 SVG 不被转码，按现有校验原样上传', () => {
  for (const mime of ['image/gif', 'image/svg+xml', 'application/pdf']) {
    assert.deepEqual(
      decideCompression(mime, 5 * 1024 * 1024, DEFAULT_COMPRESS_OPTIONS),
      { action: 'passthrough', reason: 'unsupported-format' },
    )
  }
})

test('本来就小的图不做无意义的重新编码', () => {
  assert.deepEqual(
    decideCompression('image/jpeg', 120 * 1024, DEFAULT_COMPRESS_OPTIONS),
    { action: 'passthrough', reason: 'already-small' },
  )
})

test('大 JPEG/PNG/WebP 才进入压缩流程', () => {
  for (const mime of ['image/jpeg', 'image/png', 'image/webp']) {
    assert.deepEqual(
      decideCompression(mime, 1.25 * 1024 * 1024, DEFAULT_COMPRESS_OPTIONS),
      { action: 'compress' },
    )
  }
})

test('只有确实变小才采用压缩结果', () => {
  assert.equal(isWorthKeeping(1000, 500), true)
  assert.equal(isWorthKeeping(1000, 950), false, '省不到一成不值得牺牲画质')
  assert.equal(isWorthKeeping(1000, 1200), false, '变大必须回退原图')
  assert.equal(isWorthKeeping(1000, 0), false, '编码失败必须回退原图')
})

test('浏览器不支持 WebP 编码时退回 JPEG，绝不输出错误标注的容器', () => {
  assert.equal(chooseOutputMime('image/png', false), 'image/jpeg')
  assert.equal(chooseOutputMime('image/webp', false), 'image/jpeg')
  assert.equal(chooseOutputMime('image/jpeg', true), 'image/webp')
})

test('默认长边落在要求的 1600–2000 px 区间，质量约 0.8', () => {
  assert.ok(DEFAULT_COMPRESS_OPTIONS.maxEdge >= 1600 && DEFAULT_COMPRESS_OPTIONS.maxEdge <= 2000)
  assert.ok(Math.abs(DEFAULT_COMPRESS_OPTIONS.quality - 0.8) < 0.001)
})
