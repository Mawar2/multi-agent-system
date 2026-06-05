# Quota Failover & Model Availability Plan

**Last updated:** 2026-06-05
**Status:** Design Phase

## Problem Statement

When Gemini or Claude hits rate limits/quota exhaustion, tasks should automatically fail over to the other available model tier instead of failing completely.

**Current Gap:**
- Workers are hard-coded to specific tiers (Flash→Gemini, Pro→Gemini, Claude→Claude CLI)
- No fallback when a model hits quota
- Tasks fail permanently instead of retrying with alternate model

## Goals

1. **Zero task loss** - Every task eventually completes (even if slower/more expensive)
2. **Automatic failover** - No manual intervention required
3. **Cost optimization** - Use cheaper models first, expensive as fallback
4. **Transparent** - Log all failovers for cost tracking

## Architecture Design

### Tier Hierarchy with Fallback Chain

```
Simple Task:
  Primary: Gemini Flash → Gemini Pro → Claude Sonnet (fallback chain)

Medium Task:
  Primary: Gemini Pro → Claude Sonnet → Gemini Flash (fallback chain)

Complex Task:
  Primary: Claude Sonnet → Claude Opus → Gemini Pro (fallback chain)
```

### New Components

#### 1. Model Availability Tracker

Tracks which models are currently available vs quota-exhausted:

```go
type ModelAvailability struct {
    mu              sync.RWMutex
    status          map[string]*ModelStatus  // model name → status
    quotaResetTimes map[string]time.Time     // when quota resets
}

type ModelStatus struct {
    Available      bool
    QuotaExhausted bool
    LastError      error
    LastErrorTime  time.Time
    RetryAfter     time.Time  // When to retry this model
}

// Check if model is available
func (m *ModelAvailability) IsAvailable(model string) bool {
    m.mu.RLock()
    defer m.mu.RUnlock()

    status := m.status[model]
    if status == nil {
        return true  // Unknown models assumed available
    }

    // If quota exhausted, check if reset time has passed
    if status.QuotaExhausted && time.Now().After(status.RetryAfter) {
        return true  // Quota should be reset now
    }

    return status.Available && !status.QuotaExhausted
}

// Mark model as quota exhausted
func (m *ModelAvailability) MarkQuotaExhausted(model string, retryAfter time.Duration) {
    m.mu.Lock()
    defer m.mu.Unlock()

    if m.status[model] == nil {
        m.status[model] = &ModelStatus{}
    }

    m.status[model].Available = false
    m.status[model].QuotaExhausted = true
    m.status[model].RetryAfter = time.Now().Add(retryAfter)
}
```

#### 2. Fallback Router

Routes tasks to alternate tiers when primary is unavailable:

```go
type FallbackRouter struct {
    availability *ModelAvailability
    fallbackChains map[taskqueue.Tier][]taskqueue.Tier
}

// Get next available tier for a task
func (r *FallbackRouter) GetAvailableTier(primaryTier taskqueue.Tier) taskqueue.Tier {
    // Try primary tier first
    primaryModel := r.tierToModel(primaryTier)
    if r.availability.IsAvailable(primaryModel) {
        return primaryTier
    }

    // Try fallback chain
    for _, fallbackTier := range r.fallbackChains[primaryTier] {
        model := r.tierToModel(fallbackTier)
        if r.availability.IsAvailable(model) {
            return fallbackTier
        }
    }

    // All models exhausted - return primary anyway (will fail and retry later)
    return primaryTier
}
```

#### 3. Error Detection

Detect quota errors from LLM responses:

```go
func detectQuotaError(err error) (bool, time.Duration) {
    if err == nil {
        return false, 0
    }

    errStr := strings.ToLower(err.Error())

    // Gemini quota errors
    if strings.Contains(errStr, "quota exceeded") ||
       strings.Contains(errStr, "resource exhausted") {
        return true, 1 * time.Hour  // Retry after 1 hour
    }

    // Claude rate limit errors
    if strings.Contains(errStr, "rate limit") ||
       strings.Contains(errStr, "429") {
        return true, 5 * time.Minute  // Retry after 5 minutes
    }

    // Anthropic overloaded errors
    if strings.Contains(errStr, "overloaded") {
        return true, 2 * time.Minute  // Retry after 2 minutes
    }

    return false, 0
}
```

### Workflow

**Normal Flow:**
```
1. Task routed to Tier (e.g., TierGeminiPro)
2. Worker claims task for TierGeminiPro
3. Execute with Gemini Pro model
4. Success → mark task complete
```

**Quota Exhausted Flow:**
```
1. Task routed to Tier (e.g., TierGeminiPro)
2. Worker claims task for TierGeminiPro
3. Execute with Gemini Pro model
4. Error: "quota exceeded"
5. Mark Gemini Pro as unavailable (retry in 1 hour)
6. Release task back to queue with tier=TierClaude (fallback)
7. Claude worker claims task
8. Execute with Claude Sonnet
9. Success → mark task complete
```

## Implementation Plan

### Phase 1: Detection & Tracking (Week 1)

**Files to Create:**
- `internal/orchestrator/availability.go` - ModelAvailability tracker
- `internal/orchestrator/availability_test.go` - Unit tests

**Changes:**
- `cmd/supervisor/main.go` - Initialize availability tracker
- `internal/worker/claudecode.go` - Detect quota errors, report to tracker

**Acceptance Criteria:**
- [ ] ModelAvailability tracks model status
- [ ] Quota errors detected from LLM responses
- [ ] Status persists for configured retry duration
- [ ] Unit tests covering error detection

### Phase 2: Fallback Routing (Week 2)

**Files to Create:**
- `internal/orchestrator/fallback_router.go` - FallbackRouter implementation
- `internal/orchestrator/fallback_router_test.go` - Unit tests

**Changes:**
- `orchestrator.yml` - Add fallback chain configuration
- `internal/orchestrator/supervisor.go` - Use FallbackRouter for task routing
- `internal/taskqueue/task.go` - Add `FallbackTier` field

**Acceptance Criteria:**
- [ ] Fallback chains configurable in orchestrator.yml
- [ ] Router selects alternate tier when primary unavailable
- [ ] Tasks released with new tier when quota exhausted
- [ ] Integration test with mocked quota exhaustion

### Phase 3: Monitoring & Alerts (Week 3)

**Features:**
- Dashboard showing model availability status
- Metrics for failover frequency (cost tracking)
- Alerts when all models exhausted for a tier

**Acceptance Criteria:**
- [ ] `/api/status` includes model availability
- [ ] Logs include failover events with cost implications
- [ ] Alert when all models for a complexity tier exhausted

## Configuration Format

```yaml
# orchestrator.yml

# Fallback chains (in priority order)
fallback_chains:
  gemini-flash:
    - gemini-pro      # If Flash exhausted, try Pro
    - claude-sonnet   # If Pro also exhausted, try Claude

  gemini-pro:
    - claude-sonnet   # If Pro exhausted, try Claude
    - gemini-flash    # If Claude exhausted, try Flash (slower but works)

  claude:
    - claude-opus     # If Sonnet exhausted, try Opus (more expensive)
    - gemini-pro      # If Opus exhausted, try Gemini Pro

# Quota reset times (when to retry exhausted models)
quota_reset:
  gemini-flash: 1h
  gemini-pro: 1h
  claude-sonnet: 5m   # Claude rate limits reset faster
  claude-opus: 5m
```

## Cost Optimization Strategy

**Fallback Order Philosophy:**

1. **Simple tasks** (Flash → Pro → Claude)
   - Start cheap (Flash)
   - Upgrade if needed (Pro)
   - Last resort expensive (Claude)

2. **Medium tasks** (Pro → Claude → Flash)
   - Start with capable model (Pro)
   - Upgrade for reliability (Claude)
   - Downgrade only if desperate (Flash might struggle)

3. **Complex tasks** (Claude → Opus → Pro)
   - Start with powerful model (Claude Sonnet)
   - Upgrade if quota hit (Opus - worth the cost)
   - Downgrade cautiously (Pro might fail, but better than nothing)

**Cost Tracking:**
- Log every failover with: `original_tier`, `fallback_tier`, `cost_delta`
- Weekly report: "Failovers cost extra $X this week"
- Alert if failover costs exceed threshold

## Edge Cases & Handling

### All Models Exhausted

**Scenario:** All tiers hit quota simultaneously.

**Solution:**
1. Tasks remain in queue (not failed)
2. Workers poll but find no available models
3. Log warning: "All models exhausted, waiting for quota reset"
4. Retry every 5 minutes checking quota reset times
5. As soon as any model becomes available, resume processing

### Cascading Failures

**Scenario:** Flash exhausted → everything fails over to Pro → Pro gets overwhelmed → Pro exhausted.

**Solution:**
- **Rate limiting**: Don't fail over 100% of traffic instantly
- **Gradual failover**: Fail over 10% of tasks per minute
- **Circuit breaker**: If fallback tier also starts failing, slow down

### Model-Specific Failures

**Scenario:** Gemini Flash works but Gemini Pro API is down (not quota, actual outage).

**Solution:**
- Detect non-quota errors (503, connection timeout)
- Mark model as unavailable for shorter duration (5 min vs 1 hour)
- Retry more aggressively after outages (exponential backoff)

## Metrics to Track

**Availability Metrics:**
- `model_available{model="gemini-flash"}` - 1 if available, 0 if exhausted
- `quota_exhausted_total{model="gemini-flash"}` - Counter of quota events
- `fallback_triggered_total{from="flash", to="pro"}` - Counter of failovers

**Cost Metrics:**
- `task_cost{tier="original"}` - Cost if executed on primary tier
- `task_cost{tier="fallback"}` - Actual cost after failover
- `cost_delta_total` - Total extra cost from failovers

**Performance Metrics:**
- `task_completion_time{tier="original"}` - Expected completion time
- `task_completion_time{tier="fallback"}` - Actual completion time after failover

## Success Criteria

**System is successful when:**
1. ✅ Zero tasks fail due to quota exhaustion
2. ✅ Failover happens within 30 seconds of quota error
3. ✅ All failovers logged and trackable
4. ✅ Cost impact visible in dashboards
5. ✅ System recovers automatically when quota resets
6. ✅ No manual intervention required

## Testing Plan

### Unit Tests
- Model availability state transitions
- Quota error detection from various error messages
- Fallback chain traversal logic

### Integration Tests
1. **Single model quota:** Flash exhausted → tasks route to Pro
2. **Multiple model quota:** Flash + Pro exhausted → tasks route to Claude
3. **All models exhausted:** Tasks queue up, resume when quota resets
4. **Quota reset:** Exhausted model becomes available again

### Load Tests
- 100 tasks, Flash quota = 10 → verify smooth failover to Pro
- Measure failover latency (should be < 30 seconds)
- Verify no task loss during failover

## Security Considerations

**Quota Amplification Attack:**
- Malicious actor creates 1000 simple issues
- All get routed to Flash
- Flash quota exhausted in 10 minutes
- Fail over to Pro → Pro exhausted
- Fail over to Claude → Expensive bill!

**Mitigation:**
1. **Rate limiting on issue polling** - Max 100 new issues per hour
2. **Task creation limits** - Max 50 pending tasks per project
3. **Cost alerts** - Alert if failover costs exceed $X per hour
4. **Manual approval** - Require approval for tasks estimated > $Y

## Future Enhancements (Post-MVP)

1. **Smart routing** - ML model to predict which tier will succeed
2. **Cost prediction** - Estimate cost before executing
3. **Priority-based failover** - High priority tasks fail over first
4. **Multi-region** - Fail over to different regions when quota exhausted
5. **Budget limits** - Automatically pause when budget exceeded

## Dependencies

**Required:**
- Model availability tracker (new component)
- Fallback router (new component)
- Task tier update mechanism (modify taskqueue)

**Optional but Recommended:**
- Structured logging (#5 from team tasks)
- Observability dashboard (#3 from team tasks)
- Cost tracking metrics

## Timeline

**Week 1:** Detection & Tracking (Phase 1)
**Week 2:** Fallback Routing (Phase 2)
**Week 3:** Monitoring & Alerts (Phase 3)
**Week 4:** Testing & Documentation

**Total:** 4 weeks for complete quota failover system

---

## Next Steps

1. Review this plan with team
2. Prioritize vs other team tasks (#20-#38)
3. Create GitHub issue for implementation
4. Assign to developer
5. Begin Phase 1 (Detection & Tracking)
