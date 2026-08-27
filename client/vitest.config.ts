import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

/**
 * Task23（测试与代码质量补齐）：前端此前零测试基础设施，只靠人工/playwright-cli
 * 一次性会话验证，没有可回归的自动化测试资产。本文件独立于 `vite.config.ts`
 * （生产构建配置），避免测试相关配置意外影响构建产物。
 */
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    globals: false,
    css: false,
  },
})
