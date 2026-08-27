import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { AuthProvider } from '../context/AuthContext'
import { AuthPage } from './AuthPage'

/**
 * 回归测试：用户反馈"注册账户时并不支持纯数字输入，但是没有正确提示"。
 * 复现后确认根因并非用户名限制，而是密码长度提示前后不一致——占位符/错误文案
 * 写"至少6位"，后端真实要求 8 位，用户按提示输入的密码被拒绝、错误提示还在
 * 重复错误的"6位"要求，造成困惑。这里把当时手工用 playwright-cli 验证过的行为
 * 固化为自动化回归用例，防止以后再改错。
 *
 * 注：没有用 `checkValidity()`/`.validity.tooShort` 断言 minlength 约束的实际拦截
 * 效果——jsdom 明确将 `tooShort` 硬编码为 `() => false`（见
 * `node_modules/jsdom/lib/jsdom/living/nodes/HTMLInputElement-impl.js`
 * 的注释："jsdom has no way at the moment to emulate a user interaction"），
 * 这是 jsdom 的已知限制，不是应用代码的问题。因此改为直接断言 `minLength`
 * 属性值本身，同样能覆盖"注册模式要求8位、登录模式不做限制"这个真实行为。
 */
function renderAuthPage() {
  return render(
    <MemoryRouter>
      <AuthProvider>
        <AuthPage />
      </AuthProvider>
    </MemoryRouter>,
  )
}

describe('AuthPage 密码长度与用户名规则提示', () => {
  it('注册模式：密码输入框声明 minlength=8（对应后端 minPasswordLength=8），且展示用户名规则提示', () => {
    renderAuthPage()
    fireEvent.click(screen.getByRole('button', { name: '注册' }))

    expect(screen.getByText('3-32 位，支持字母、数字、下划线（纯数字也可以）')).toBeInTheDocument()

    const passwordInput = screen.getByPlaceholderText('密码（至少8位，需同时包含字母和数字）') as HTMLInputElement
    expect(passwordInput.minLength).toBe(8)
  })

  it('登录模式：不强制密码最小长度（避免误伤历史账号/访客场景）', () => {
    renderAuthPage()
    const passwordInput = screen.getByPlaceholderText('密码（至少8位，需同时包含字母和数字）') as HTMLInputElement
    expect(passwordInput.hasAttribute('minlength')).toBe(false)
  })

  it('纯数字用户名可以正常输入且通过浏览器原生校验（用户名本身从未受限）', () => {
    renderAuthPage()
    fireEvent.click(screen.getByRole('button', { name: '注册' }))

    const usernameInput = screen.getByPlaceholderText('用户名') as HTMLInputElement
    fireEvent.change(usernameInput, { target: { value: '888888' } })
    expect(usernameInput.value).toBe('888888')
    expect(usernameInput.checkValidity()).toBe(true)
  })
})
