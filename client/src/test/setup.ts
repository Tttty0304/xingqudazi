// Vitest 全局测试环境设置。
import '@testing-library/jest-dom/vitest'
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'

// `vitest.config.ts` 里关闭了 `globals`（显式 import 优先，避免污染全局命名空间），
// 因此 testing-library 的自动 cleanup 检测不到全局 afterEach，需要在这里手动注册，
// 否则每个 it() 渲染出的 DOM 会在同一测试文件内的多个用例之间残留、互相干扰。
afterEach(() => {
  cleanup()
})
