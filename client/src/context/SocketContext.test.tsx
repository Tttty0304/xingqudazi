import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, act, fireEvent } from '@testing-library/react'
import { SocketProvider, useSocket } from './SocketContext'

// SocketContext 依赖 `useAuth()` 拿 token 才会建立连接；这里用一个固定 token
// 的假实现替换，让测试不依赖真实的 AuthProvider/localStorage 状态。
vi.mock('./AuthContext', () => ({
  useAuth: () => ({ token: 'fake-token-for-test' }),
}))

/** 最小可控的假 WebSocket：不发真实网络请求，把 open/close 时机交给测试代码手动驱动。 */
class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSING = 2
  static readonly CLOSED = 3

  url: string
  readyState = FakeWebSocket.CONNECTING
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
	sent: string[] = []

  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }

	send(payload: string) {
		this.sent.push(payload)
	}

  /** 供 SocketProvider 卸载时调用的“主动关闭”。 */
  close() {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.()
  }

  /** 测试用：模拟服务端握手成功。 */
  simulateOpen() {
    this.readyState = FakeWebSocket.OPEN
    this.onopen?.()
  }

  /** 测试用：模拟网络抖动/服务端重启导致的非主动断线。 */
  simulateDrop() {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.()
  }
}

function StatusProbe() {
  const { status } = useSocket()
  return <div data-testid="status">{status}</div>
}

function ReliableSendProbe() {
	const { sendReliable } = useSocket()
	return (
		<button
			onClick={() => sendReliable({ type: 'send_message', room_id: '00000000-0000-0000-0000-000000000001', msg_id: '00000000-0000-0000-0000-000000000002', content: 'hello' })}
		>
			可靠发送
		</button>
	)
}

describe('SocketContext 断线自动重连（能力补齐：此前断线后需要用户手动刷新页面）', () => {
  const OriginalWebSocket = globalThis.WebSocket

  beforeEach(() => {
    FakeWebSocket.instances = []
    // @ts-expect-error 测试用假实现整体替换全局 WebSocket 构造函数
    globalThis.WebSocket = FakeWebSocket
    vi.useFakeTimers()
  })

  afterEach(() => {
    globalThis.WebSocket = OriginalWebSocket
    vi.useRealTimers()
  })

  it('非主动断线后按指数退避（1s）自动重建连接，连接恢复后 status 变回 open', () => {
    render(
      <SocketProvider>
        <StatusProbe />
      </SocketProvider>,
    )

    expect(FakeWebSocket.instances).toHaveLength(1)
    const first = FakeWebSocket.instances[0]

    act(() => first.simulateOpen())
    expect(screen.getByTestId('status').textContent).toBe('open')

    // 模拟网络抖动：非主动关闭。
    act(() => first.simulateDrop())
    expect(screen.getByTestId('status').textContent).toBe('reconnecting')
    expect(FakeWebSocket.instances).toHaveLength(1) // 重连定时器还没触发，不应立刻新建连接

    act(() => {
      vi.advanceTimersByTime(1000) // 第一次重连的退避延迟是 1s
    })
    expect(FakeWebSocket.instances).toHaveLength(2)

    const second = FakeWebSocket.instances[1]
    act(() => second.simulateOpen())
    expect(screen.getByTestId('status').textContent).toBe('open')
  })

  it('连续多次断线时退避延迟指数增长（1s -> 2s），不会无限制立刻重连', () => {
    render(
      <SocketProvider>
        <StatusProbe />
      </SocketProvider>,
    )

    const first = FakeWebSocket.instances[0]
    act(() => first.simulateOpen())
    act(() => first.simulateDrop())

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(FakeWebSocket.instances).toHaveLength(2)
    const second = FakeWebSocket.instances[1]

    // 第二次连接还没握手成功就又掉线：验证退避延迟变成 2s 而不是又是 1s。
    act(() => second.simulateDrop())
    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(FakeWebSocket.instances).toHaveLength(2) // 1s 还不够，不应新建

    act(() => {
      vi.advanceTimersByTime(1000) // 累计 2s，达到第二次退避延迟
    })
    expect(FakeWebSocket.instances).toHaveLength(3)
  })

  it('组件卸载（主动关闭）时不会触发自动重连', () => {
    const { unmount } = render(
      <SocketProvider>
        <StatusProbe />
      </SocketProvider>,
    )

    const first = FakeWebSocket.instances[0]
    act(() => first.simulateOpen())

    act(() => unmount())

    act(() => {
      vi.advanceTimersByTime(20000)
    })
    // 主动关闭（组件卸载）不应触发重连逻辑，连接数应保持为 1。
    expect(FakeWebSocket.instances).toHaveLength(1)
  })

	it('收到同一 msg_id 的服务端回显后确认送达，不会再重放该消息', () => {
		render(
			<SocketProvider>
				<ReliableSendProbe />
			</SocketProvider>,
		)
		const ws = FakeWebSocket.instances[0]
		act(() => ws.simulateOpen())

		fireEvent.click(screen.getByText('可靠发送'))
		expect(ws.sent).toHaveLength(1)
		expect(JSON.parse(ws.sent[0]).msg_id).toBe('00000000-0000-0000-0000-000000000002')

		act(() => ws.onmessage?.(new MessageEvent('message', { data: JSON.stringify({ type: 'message_received', msg_id: '00000000-0000-0000-0000-000000000002' }) })))
		act(() => vi.advanceTimersByTime(10000))
		expect(ws.sent).toHaveLength(1)
	})
})
