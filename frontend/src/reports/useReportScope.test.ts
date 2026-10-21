import { computed, ref } from 'vue'
import { describe, expect, it } from 'vitest'
import { queryKey, useReportScope } from './useReportScope'
describe('report filter scope',()=>{
  it('accepts only the filters that actually produced the loaded data',()=>{
    const window=ref(30),query=computed(()=>({window_days:window.value,status:''}))
    const scope=useReportScope(query);expect(scope.dirty.value).toBe(true)
    const request={...query.value};window.value=10;scope.accept(request)
    expect(scope.dirty.value).toBe(true);expect(scope.applied.value?.window_days).toBe(30)
    scope.accept({...query.value});expect(scope.dirty.value).toBe(false)
  })
  it('ignores empty values and field ordering',()=>{
    expect(queryKey({a:1,b:'',c:null})).toBe(queryKey({a:'1'}))
  })
})
