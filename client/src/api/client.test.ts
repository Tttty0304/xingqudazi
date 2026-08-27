import { describe, it, expect } from 'vitest'
import { ApiError, errorMessage, newIdempotencyKey, resolveMediaUrl, resolveWsUrl, serverOrigin, translateCode } from './client'

// 测试运行环境未设置 VITE_API_BASE_URL，`API_BASE` 回落到默认值
// 'http://localhost:8080'（见 client.ts），以下断言均基于这个默认值。

describe('serverOrigin / resolveWsUrl / resolveMediaUrl', () => {
  it('serverOrigin 在 API_BASE 为绝对 URL 时返回其 origin', () => {
    expect(serverOrigin()).toBe('http://localhost:8080')
  })

  it('resolveWsUrl 把 http(s) 协议替换为 ws(s) 并拼接 /ws（后端 /ws 不带 /api 前缀）', () => {
    expect(resolveWsUrl()).toBe('ws://localhost:8080/ws')
  })

  it('resolveMediaUrl 对已是绝对 URL 的路径原样返回', () => {
    const absolute = 'https://cdn.example.com/img.png'
    expect(resolveMediaUrl(absolute)).toBe(absolute)
  })

  it('resolveMediaUrl 把后端返回的相对路径拼接为可用的绝对 URL', () => {
    expect(resolveMediaUrl('/uploads/abc.png')).toBe('http://localhost:8080/uploads/abc.png')
  })
})

describe('translateCode / errorMessage', () => {
  it('translateCode 命中已知错误码时返回中文提示', () => {
    expect(translateCode('invalid_password')).toBe('密码不符合要求（至少8位，需同时包含字母和数字）')
    expect(translateCode('login_rate_limited')).toBe('登录尝试过于频繁，请稍后再试')
  })

  it('translateCode 遇到未知错误码时原样返回（不吞掉信息，方便排查未覆盖的新错误码）', () => {
    expect(translateCode('some_brand_new_error_code')).toBe('some_brand_new_error_code')
  })

  it('errorMessage 对 ApiError 走 translateCode 翻译', () => {
    const err = new ApiError(400, 'invalid_username')
    expect(errorMessage(err)).toBe('用户名格式不正确')
  })

  it('errorMessage 对普通 Error 返回其 message', () => {
    expect(errorMessage(new Error('network down'))).toBe('network down')
  })

  it('errorMessage 对非 Error 值转成字符串', () => {
    expect(errorMessage('plain string failure')).toBe('plain string failure')
  })
})

describe('newIdempotencyKey', () => {
  it('生成符合后端最小长度要求的写命令重放标识', () => {
    const key = newIdempotencyKey()
    expect(key).toMatch(/^[A-Za-z0-9._-]{8,128}$/)
  })
})
