import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import StatusBadge from './StatusBadge.vue'

describe('StatusBadge', () => {
  it('renders localized evaluation status', () => {
    const wrapper = mount(StatusBadge, { props: { status: 'READY' } })
    expect(wrapper.text()).toContain('已评估')
    expect(wrapper.classes()).toContain('status-ready')
  })

  it('falls back to the raw status', () => {
    const wrapper = mount(StatusBadge, { props: { status: 'CUSTOM' } })
    expect(wrapper.text()).toContain('CUSTOM')
  })
})
