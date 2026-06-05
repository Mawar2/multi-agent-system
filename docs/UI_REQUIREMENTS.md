# UI/UX Requirements Specification
# Multi-Agent Orchestration Dashboard

**Version:** 1.0
**Date:** 2026-06-05
**Target Audience:** UI/UX Designers, Frontend Developers

---

## 1. Executive Summary

This document specifies the UI/UX requirements for a real-time monitoring dashboard for the Multi-Agent Orchestration System. The dashboard enables operators to monitor autonomous AI workers processing GitHub Issues, track task queues across complexity tiers, view success/failure rates, manage costs, and control system operations.

**Key Users:**
- **Product Owners/Team Leads** - Monitor overall system health and productivity
- **DevOps Engineers** - Troubleshoot failures, manage worker pools, control supervisor
- **Finance/Management** - Track API costs and ROI metrics

**Core Value Proposition:**
Transform developers into Product Owners by providing full visibility and control over autonomous agents handling ticket implementation across multiple projects.

---

## 2. Dashboard Overview

### 2.1 Screen Structure

The dashboard consists of **5 primary views** accessible via top navigation:

1. **Home Dashboard** - Real-time overview (default landing page)
2. **Task Queue** - Detailed task list with filtering and search
3. **Workers** - Worker status, health monitoring, and assignment tracking
4. **Projects** - Multi-project management and per-project metrics
5. **Analytics** - Historical data, trends, and cost analysis

### 2.2 Layout Pattern

**Desktop Layout:**
```
┌─────────────────────────────────────────────────────┐
│  Logo   Home | Queue | Workers | Projects | Analytics│  [User Menu]
├─────────────────────────────────────────────────────┤
│                                                       │
│  [Left Sidebar - Filters/Controls]  [Main Content]   │
│                                                       │
│                                                       │
└─────────────────────────────────────────────────────┘
```

**Mobile Layout:**
- Hamburger menu for navigation
- Bottom tab bar for primary views
- Swipe gestures for switching between views
- Collapsible sections to save vertical space

### 2.3 Design System Requirements

**Color Palette:**
- **Primary:** Blue (#0066CC) - For primary actions, headers
- **Success:** Green (#28A745) - Completed tasks, healthy workers
- **Warning:** Yellow/Orange (#FFC107) - In-progress tasks, warnings
- **Error:** Red (#DC3545) - Failed tasks, unhealthy workers
- **Neutral:** Gray scale (#212529 to #F8F9FA) - Background, text

**Typography:**
- **Headers:** Bold, sans-serif (e.g., Inter, SF Pro, Roboto)
- **Body:** Regular, sans-serif, 14-16px base
- **Monospace:** For task IDs, branch names, code snippets

**Components:**
- Status badges (rounded pills with icon + text)
- Cards with subtle shadows for metrics
- Progress bars for queue depth and completion rates
- Real-time updating counters with smooth animations
- Toast notifications for alerts (non-intrusive)

---

## 3. Real-Time Metrics Display (Home Dashboard)

### 3.1 Hero Metrics (Top Row)

Display 4-6 large metric cards prominently at the top:

**Metric 1: System Status**
- **Visual:** Large status indicator (green circle = running, red = stopped, yellow = degraded)
- **Text:** "OPERATIONAL" | "STOPPED" | "DEGRADED"
- **Sub-text:** Uptime (e.g., "Running for 2h 15m")
- **Action:** Button to start/stop supervisor

**Metric 2: Active Workers**
- **Visual:** Counter with icon (e.g., robot emoji or worker icon)
- **Text:** "8 / 10" (active / total capacity)
- **Sub-text:** Breakdown by tier: "Flash: 3 | Pro: 3 | Claude: 2"
- **Action:** Click to navigate to Workers view

**Metric 3: Queue Depth**
- **Visual:** Counter with queue icon
- **Text:** "14 tasks pending"
- **Sub-text:** "6 in progress, 8 waiting"
- **Action:** Click to navigate to Task Queue view

**Metric 4: Success Rate**
- **Visual:** Percentage with trend arrow (↑ or ↓)
- **Text:** "68%" (21 successful / 31 total)
- **Sub-text:** "Last 24 hours"
- **Color:** Green if > 75%, yellow if 50-75%, red if < 50%

**Metric 5: PRs Created Today**
- **Visual:** Counter with GitHub PR icon
- **Text:** "10 PRs"
- **Sub-text:** "3 merged, 7 awaiting review"
- **Action:** Click to see PR list

**Metric 6: Cost Today**
- **Visual:** Currency amount with cost icon
- **Text:** "$5.13"
- **Sub-text:** "Claude: $3.00 | Gemini Pro: $2.10 | Flash: $0.03"
- **Action:** Click to navigate to Analytics > Cost Tracking

### 3.2 Live Activity Feed (Below Hero Metrics)

**Purpose:** Show real-time events as they happen (newest first)

**Feed Item Structure:**
```
[Timestamp] [Icon] [Event Message] [Action Link]

Example entries:
13:45:12  ✅  Worker claude-2 completed task #16 → PR #22        [View PR]
13:43:08  🔄  Worker gemini-pro-1 claimed task #11              [View Task]
13:40:22  ❌  Worker gemini-flash-2 failed task #19 (extraction) [View Error]
13:38:15  📥  Supervisor enqueued issue #21 → Complex tier      [View Issue]
```

**Features:**
- Auto-scroll to top when new events arrive (with animation)
- "Pause Feed" button to stop auto-scroll for inspection
- Filter by event type (success, failure, claim, enqueue)
- Limit to last 50 events (load more button)

### 3.3 Quick Stats Grid (Below Activity Feed)

Display secondary metrics in a 3-column grid:

**Column 1: Throughput**
- Tasks completed per hour: "8.5 tasks/hr"
- Average task duration: "12m 30s"
- Fastest completion: "3m 15s (issue #18)"

**Column 2: Quality**
- Test pass rate: "95% (19/20)"
- Linter pass rate: "100% (20/20)"
- Retry rate: "10% (3/31 tasks retried)"

**Column 3: System Health**
- API success rate: "92% (23/25 GitHub calls)"
- Worker uptime: "99.2%"
- Queue latency: "< 5 seconds"

---

## 4. Task Queue Visualization

### 4.1 Queue Overview (Top Section)

**Visual:** 3 columns representing the 3 worker tiers

```
┌──────────────────┬──────────────────┬──────────────────┐
│  Gemini Flash    │   Gemini Pro     │     Claude       │
│   (Simple)       │   (Medium)       │   (Complex)      │
├──────────────────┼──────────────────┼──────────────────┤
│  5 Pending       │  12 Pending      │  7 Pending       │
│  2 In Progress   │  4 In Progress   │  2 In Progress   │
│  15 Completed    │  21 Completed    │  10 Completed    │
├──────────────────┼──────────────────┼──────────────────┤
│  [View Tasks]    │  [View Tasks]    │  [View Tasks]    │
└──────────────────┴──────────────────┴──────────────────┘
```

**Features:**
- Click column to filter task list by tier
- Color-coded by status (pending = gray, in progress = yellow, completed = green)
- Progress bars showing queue depth vs. capacity

### 4.2 Task List (Main Section)

**Table Columns:**
1. **Status** - Badge (Pending | Claimed | In Progress | Review | Complete | Failed)
2. **Task ID** - Short UUID (e.g., "0aebc0c...") with copy button
3. **Issue** - GitHub issue number and title (link to GitHub)
4. **Tier** - Badge (Flash | Pro | Claude) with color coding
5. **Worker** - Worker ID if claimed, else "-"
6. **Age** - Time since enqueued (e.g., "15m ago")
7. **Duration** - Time from claimed to completed (if applicable)
8. **Actions** - View Details | Retry (if failed) | Release (if stuck)

**Features:**
- **Sorting:** Click column headers to sort
- **Filtering:** Sidebar filters for status, tier, project, age range
- **Search:** Full-text search across title, description, issue number
- **Pagination:** 25 tasks per page (configurable)
- **Bulk Actions:** Select multiple tasks → Release, Delete, Retry

**Row Click Behavior:**
Clicking a row opens a **Task Detail Modal** (see 4.3)

### 4.3 Task Detail Modal

**Layout:** Full-screen modal with close button

**Sections:**

**Header:**
- Task ID (with copy button)
- Status badge (large, prominent)
- Issue link (GitHub icon + #number + title)

**Metadata (2-column grid):**
- Repository: `Mawar2/Kaimi`
- Tier: Gemini Pro (Medium)
- Complexity: 1 (Medium)
- Worker: gemini-pro-3
- Claimed at: 2026-06-05 14:09:25
- Started at: 2026-06-05 14:09:25
- Completed at: Not yet
- Attempts: 0
- Retry limit: 3

**Description:**
- Full issue body (markdown rendered)
- Acceptance criteria checklist (extracted, show completion status)

**Timeline (Event Log):**
- Enqueued by supervisor at [timestamp]
- Claimed by worker gemini-pro-3 at [timestamp]
- Started execution at [timestamp]
- (Future events appear as they happen)

**Actions:**
- Retry Task (if failed)
- Release Task (if in progress)
- View PR (if PR created)
- View Logs (if logs available)
- Cancel Task

### 4.4 Status Flow Visualization

**Visual:** Horizontal timeline showing task lifecycle

```
Pending → Claimed → In Progress → Review → Complete
   ↓                    ↓
 Failed             Released
```

**Features:**
- Current status highlighted
- Timestamps shown at each transition
- Click stage to see event details

---

## 5. Worker Status Panel

### 5.1 Worker Grid (Main View)

**Layout:** Grid of worker cards (3 per row on desktop, 1 on mobile)

**Worker Card Structure:**

```
┌────────────────────────────────────┐
│  [Status Circle] gemini-pro-1      │
│  Tier: Gemini Pro                  │
│  Status: Working on #11            │
│  ────────────────────────────────  │
│  Completed: 6 | Failed: 1          │
│  Success Rate: 86%                 │
│  Avg Duration: 15m 30s             │
│  ────────────────────────────────  │
│  Last Heartbeat: 2s ago            │
│  [View Details] [Restart]          │
└────────────────────────────────────┘
```

**Status Circle Colors:**
- Green: Healthy and working
- Yellow: Healthy but idle (no tasks)
- Red: Unhealthy (hung, crashed, no heartbeat)
- Gray: Offline/stopped

**Features:**
- Click card to expand worker details (modal or right sidebar)
- Filter by tier, status, health
- Sort by completion count, success rate, age

### 5.2 Worker Detail View

**Modal/Sidebar with tabs:**

**Tab 1: Overview**
- Worker ID
- Tier and model (e.g., "Gemini Pro 3.5")
- Status and health
- Current assignment (task ID + issue)
- Statistics (completed, failed, success rate)
- Last heartbeat timestamp

**Tab 2: Task History**
- List of all tasks this worker has claimed (most recent first)
- Include status, duration, outcome
- Link to view task details

**Tab 3: Health Metrics**
- Uptime
- Average task duration
- Failure reasons (if any failures)
- Resource usage (if tracked - CPU, memory)

**Tab 4: Logs**
- Real-time log stream for this worker
- Filter by level (debug, info, warn, error)
- Search logs
- Download logs

**Actions:**
- Restart Worker
- Stop Worker
- View Current Task
- Force Release Current Task

### 5.3 Tier Utilization Chart

**Visual:** 3 horizontal bars showing capacity utilization

```
Gemini Flash:  [████░░] 4 / 5 workers (80%)
Gemini Pro:    [███░░░] 3 / 3 workers (100%) ⚠️ At capacity
Claude:        [█░░░░] 1 / 2 workers (50%)
```

**Features:**
- Color: Green if < 80%, yellow if 80-100%, red if = 100%
- Click bar to filter worker grid by tier
- Alert icon (⚠️) if tier at capacity with pending tasks

---

## 6. Project Management

### 6.1 Project List

**Layout:** List or grid of configured projects

**Project Card:**
```
┌─────────────────────────────────────────┐
│  Kaimi                                  │
│  Mawar2/Kaimi                           │
│  ─────────────────────────────────────  │
│  Active Tasks: 8                        │
│  Completed Today: 10 PRs                │
│  Success Rate: 68%                      │
│  ─────────────────────────────────────  │
│  [View Details] [Configure] [Disable]   │
└─────────────────────────────────────────┘
```

**Features:**
- Add new project (+ button)
- Enable/disable project monitoring
- View per-project metrics
- Configure project settings

### 6.2 Project Detail View

**Sections:**

**Configuration:**
- Repository (owner/name)
- Conventions path (path to CLAUDE.md)
- Branch pattern (e.g., "feature/KAI-{ticket}-{summary}")
- Commit pattern (e.g., "{ticket}_{description}")
- Label filters (optional)
- Poll interval override (optional)

**Metrics (Last 24 Hours):**
- Tasks enqueued: 31
- Tasks completed: 21
- PRs created: 10
- Success rate: 68%
- Average task duration: 12m 30s

**Recent PRs:**
- Table of PRs created by workers for this project
- Columns: PR #, Issue #, Title, Worker, Created At, Status (open/merged)
- Link to GitHub PR

**Recent Failures:**
- Table of failed tasks for this project
- Columns: Task ID, Issue #, Worker, Error Message, Failed At
- Link to task detail

**Actions:**
- Edit configuration
- Test configuration (validate conventions file, GitHub access)
- Disable project
- Delete project

### 6.3 Add/Edit Project Form

**Form Fields:**
- **Name:** Project name (e.g., "Kaimi")
- **Repository:** Owner and repo (e.g., "Mawar2/Kaimi")
- **Conventions Path:** Path to CLAUDE.md or CONVENTIONS.md
- **Branch Pattern:** Template (e.g., "feature/{ticket}-{summary}")
- **Commit Pattern:** Template (e.g., "{ticket}_{description}")
- **Label Filters:** (Optional) Filter issues by labels
- **Enabled:** Toggle switch (on/off)

**Validation:**
- Test GitHub access (can read issues?)
- Test conventions file (exists and parseable?)
- Show validation errors inline

**Actions:**
- Save & Enable
- Save as Draft (disabled)
- Cancel

---

## 7. Cost Tracking

### 7.1 Cost Summary (Analytics View - Top)

**Hero Metrics:**
- **Today's Cost:** $5.13 (large, prominent)
- **This Week:** $35.91
- **This Month:** $142.50
- **Projected Monthly:** $158.00 (based on current rate)

**Breakdown Pie Chart:**
- Claude (Complex): 58% ($3.00)
- Gemini Pro (Medium): 41% ($2.10)
- Gemini Flash (Simple): 1% ($0.03)

### 7.2 Cost by Tier (Table)

**Columns:**
1. **Tier** - Flash | Pro | Claude
2. **Tasks Completed** - Count
3. **Total Cost** - Dollar amount
4. **Cost per Task** - Average cost
5. **Percentage** - % of total cost

**Example:**
```
Tier          Tasks  Total Cost  Avg Cost  % of Total
───────────────────────────────────────────────────
Claude          6      $3.00      $0.50      58%
Gemini Pro     21      $2.10      $0.10      41%
Gemini Flash    3      $0.03      $0.01       1%
───────────────────────────────────────────────────
TOTAL          30      $5.13      $0.17     100%
```

### 7.3 Cost by Project (Table)

**Columns:**
1. **Project** - Project name
2. **Tasks Completed** - Count
3. **Total Cost** - Dollar amount
4. **Avg Cost per Task** - Average
5. **Trend** - Arrow showing cost trend (↑ or ↓)

### 7.4 Cost Trend Chart (Line Graph)

**Visual:** Line chart showing cost over time

**Options:**
- Time range selector: Today | Week | Month | Year
- Granularity: Hourly | Daily | Weekly
- Overlay: Tasks completed (secondary Y-axis)
- Split by tier: 3 lines (Flash, Pro, Claude)

### 7.5 Failover Cost Tracking (Future)

**Purpose:** Track costs when quota failover occurs (e.g., Gemini Pro quota exceeded, fallback to Claude)

**Metrics:**
- Failover events count
- Additional cost from failover
- Percentage of tasks using failover
- Failover reasons (quota exhausted, model unavailable)

**Alert:** Show warning if failover cost > 20% of total cost

---

## 8. Controls & Actions

### 8.1 Supervisor Controls (Top Right)

**Start/Stop Button:**
- Visual: Large toggle button (green = running, red = stopped)
- Text: "Stop Supervisor" (when running) | "Start Supervisor" (when stopped)
- Confirmation modal before stopping (warn about in-progress tasks)

**Pause Workers Button:**
- Visual: Pause icon button (yellow)
- Action: Pause all workers from claiming new tasks (finish current tasks first)
- Text: "Pause Workers" | "Resume Workers"

**Emergency Stop Button:**
- Visual: Red button with warning icon
- Action: Immediately stop all workers and supervisor (force quit)
- Confirmation: "This will abort all in-progress tasks. Are you sure?"

### 8.2 Task Actions (Task Queue View)

**Individual Task Actions (Dropdown Menu):**
- View Details
- View Issue (link to GitHub)
- View PR (if PR created)
- Retry Task (if failed)
- Release Task (if claimed/in-progress)
- Cancel Task
- Delete Task (if terminal status)

**Bulk Task Actions (Selected Tasks):**
- Retry Selected (for failed tasks)
- Release Selected (for stuck tasks)
- Delete Selected (for terminal tasks)

### 8.3 Worker Actions (Worker View)

**Individual Worker Actions (Dropdown Menu):**
- View Details
- Restart Worker
- Stop Worker
- Force Release Current Task
- View Logs

**Bulk Worker Actions (Selected Workers):**
- Restart Selected
- Stop Selected

### 8.4 Project Actions (Project View)

**Individual Project Actions (Dropdown Menu):**
- View Details
- Edit Configuration
- Test Configuration
- Disable Project
- Delete Project

---

## 9. Alerts & Notifications

### 9.1 Alert Triggers

**Critical Alerts (Red):**
- Supervisor stopped unexpectedly
- All workers in a tier are unhealthy
- Task queue backing up (> 50 pending for > 30 minutes)
- API rate limit exceeded (GitHub API throttled)
- Cost exceeded daily budget threshold

**Warning Alerts (Yellow):**
- Worker unhealthy (no heartbeat for 5+ minutes)
- Task stalled (in progress > 2 hours)
- High failure rate (> 50% in last hour)
- Single tier at capacity with pending tasks

**Info Alerts (Blue):**
- PR created successfully
- Task completed successfully
- New project added
- Configuration updated

### 9.2 Notification Display

**Toast Notifications:**
- Appear in top-right corner
- Auto-dismiss after 5 seconds (critical alerts stay longer)
- Click to view details
- Stack vertically (max 3 visible)

**Notification Center:**
- Bell icon in top nav (with badge count)
- Click to open notification panel (slide-in from right)
- List all alerts (newest first)
- Filter by severity (all | critical | warning | info)
- Mark as read / Clear all

### 9.3 Alert Configuration (Settings)

**Per Alert Type:**
- Enable/disable alert
- Notification method: Dashboard | Email | Slack (future)
- Threshold (e.g., "Alert if > X pending tasks")
- Frequency (e.g., "Alert once per hour max")

---

## 10. Historical Data & Analytics

### 10.1 Metrics Over Time

**Charts Available:**

**1. Task Throughput (Line Chart)**
- X-axis: Time
- Y-axis: Tasks completed per hour
- Split by tier (3 lines)
- Time range selector: Day | Week | Month | Year

**2. Success Rate (Line Chart)**
- X-axis: Time
- Y-axis: Success rate percentage
- Split by tier or overall
- Show 7-day moving average

**3. Worker Utilization (Stacked Area Chart)**
- X-axis: Time
- Y-axis: Number of active workers
- Stacked by tier (Flash, Pro, Claude)
- Show capacity line as reference

**4. Queue Depth (Area Chart)**
- X-axis: Time
- Y-axis: Number of pending tasks
- Color zones: Green (< 10), yellow (10-30), red (> 30)

**5. Average Task Duration (Bar Chart)**
- X-axis: Time period (day/week)
- Y-axis: Average duration in minutes
- Group by tier

### 10.2 Performance Summary Table

**Columns:**
1. **Period** - Date range (e.g., "June 1-7, 2026")
2. **Tasks Completed** - Count
3. **PRs Created** - Count
4. **Success Rate** - Percentage
5. **Avg Duration** - Time
6. **Total Cost** - Dollar amount
7. **Cost per PR** - Calculated

**Features:**
- Export to CSV
- Compare periods (select 2 rows to compare)
- Drill down to day-level data

### 10.3 Trends & Insights (Auto-Generated)

**Purpose:** AI-generated insights based on historical data

**Examples:**
- "Success rate improved 12% this week compared to last week"
- "Claude tier is underutilized (25% capacity) - consider reducing max workers"
- "Issue #18 completed in 3m 15s - fastest completion this month"
- "Average queue depth increased 40% this week - consider adding workers"

**Visual:** Card-based insights with icon, trend arrow, and text

---

## 11. Data Models (For Backend API)

### 11.1 System Status Model

```json
{
  "system": {
    "status": "running" | "stopped" | "degraded",
    "uptime_seconds": 8100,
    "started_at": "2026-06-05T12:00:00Z"
  },
  "workers": {
    "total": 10,
    "active": 8,
    "idle": 2,
    "unhealthy": 0,
    "by_tier": {
      "gemini_flash": { "active": 3, "max": 5 },
      "gemini_pro": { "active": 3, "max": 3 },
      "claude": { "active": 2, "max": 2 }
    }
  },
  "queue": {
    "pending": 14,
    "in_progress": 6,
    "completed_today": 21,
    "failed_today": 3
  },
  "metrics": {
    "success_rate": 0.68,
    "avg_duration_seconds": 750,
    "tasks_per_hour": 8.5,
    "prs_created_today": 10
  },
  "cost": {
    "today": 5.13,
    "this_week": 35.91,
    "this_month": 142.50
  }
}
```

### 11.2 Task Model

```json
{
  "id": "0aebc0cf-2ac9-4d36-bd92-1f93dc1bf424",
  "issue_number": 19,
  "repo_owner": "Mawar2",
  "repo_name": "Kaimi",
  "title": "KAI-M3: Upgrade Hunter to gate on eligibility",
  "description": "## Summary\n...",
  "complexity": "medium",
  "tier": "gemini_pro",
  "status": "in_progress",
  "worker_id": "gemini-pro-3",
  "branch_name": null,
  "pr_number": null,
  "claimed_at": "2026-06-05T14:09:25Z",
  "started_at": "2026-06-05T14:09:25Z",
  "completed_at": null,
  "attempts": 0,
  "error_msg": null,
  "logs_path": null,
  "metadata": {}
}
```

### 11.3 Worker Model

```json
{
  "id": "gemini-pro-3",
  "tier": "gemini_pro",
  "model": "gemini-pro-3.5",
  "status": "working",
  "health": {
    "healthy": true,
    "last_heartbeat": "2026-06-05T14:30:15Z",
    "error_msg": null
  },
  "current_task": {
    "task_id": "0aebc0cf-2ac9-4d36-bd92-1f93dc1bf424",
    "issue_number": 19,
    "started_at": "2026-06-05T14:09:25Z"
  },
  "stats": {
    "tasks_completed": 6,
    "tasks_failed": 1,
    "success_rate": 0.86,
    "avg_duration_seconds": 930,
    "uptime_seconds": 8100
  }
}
```

### 11.4 Project Model

```json
{
  "name": "kaimi",
  "repo_owner": "Mawar2",
  "repo_name": "Kaimi",
  "conventions_path": "./CLAUDE.md",
  "branch_pattern": "feature/KAI-{ticket}-{summary}",
  "commit_pattern": "{ticket}_{description}",
  "labels": [],
  "enabled": true,
  "metrics": {
    "tasks_enqueued": 31,
    "tasks_completed": 21,
    "prs_created": 10,
    "success_rate": 0.68,
    "avg_duration_seconds": 750
  }
}
```

### 11.5 Activity Event Model

```json
{
  "id": "evt-12345",
  "timestamp": "2026-06-05T13:45:12Z",
  "event_type": "task_completed" | "task_claimed" | "task_failed" | "task_enqueued" | "pr_created",
  "severity": "info" | "warning" | "error",
  "message": "Worker claude-2 completed task #16 → PR #22",
  "data": {
    "worker_id": "claude-2",
    "task_id": "0aebc0cf...",
    "issue_number": 16,
    "pr_number": 22
  },
  "action_url": "/tasks/0aebc0cf..."
}
```

### 11.6 Cost Breakdown Model

```json
{
  "period": "today" | "week" | "month",
  "total": 5.13,
  "by_tier": {
    "gemini_flash": {
      "tasks": 3,
      "cost": 0.03,
      "avg_cost_per_task": 0.01
    },
    "gemini_pro": {
      "tasks": 21,
      "cost": 2.10,
      "avg_cost_per_task": 0.10
    },
    "claude": {
      "tasks": 6,
      "cost": 3.00,
      "avg_cost_per_task": 0.50
    }
  },
  "by_project": {
    "kaimi": {
      "tasks": 30,
      "cost": 5.13
    }
  }
}
```

---

## 12. API Endpoints (For Frontend)

### 12.1 Real-Time Endpoints

**GET /api/status**
- Returns: System status, worker counts, queue depth, metrics
- Refresh: Every 5 seconds

**GET /api/activity**
- Returns: Recent activity events (last 50)
- Refresh: Every 2 seconds (or use WebSocket)

**WebSocket /ws/events**
- Real-time event stream
- Events: task_claimed, task_completed, task_failed, worker_status_changed, etc.

### 12.2 Task Endpoints

**GET /api/tasks**
- Query params: status, tier, project, limit, offset, search
- Returns: Paginated task list

**GET /api/tasks/:id**
- Returns: Full task details including timeline

**POST /api/tasks/:id/retry**
- Action: Retry failed task

**POST /api/tasks/:id/release**
- Action: Release claimed/in-progress task

**DELETE /api/tasks/:id**
- Action: Delete terminal task

### 12.3 Worker Endpoints

**GET /api/workers**
- Query params: tier, status, health
- Returns: List of workers with current status

**GET /api/workers/:id**
- Returns: Full worker details including history and logs

**POST /api/workers/:id/restart**
- Action: Restart worker

**POST /api/workers/:id/stop**
- Action: Stop worker

**GET /api/workers/:id/logs**
- Query params: level, since, limit
- Returns: Worker logs

### 12.4 Project Endpoints

**GET /api/projects**
- Returns: List of configured projects

**GET /api/projects/:name**
- Returns: Full project details and metrics

**POST /api/projects**
- Body: Project configuration
- Action: Add new project

**PUT /api/projects/:name**
- Body: Updated configuration
- Action: Update project

**DELETE /api/projects/:name**
- Action: Delete project

**POST /api/projects/:name/test**
- Action: Validate project configuration

### 12.5 Analytics Endpoints

**GET /api/metrics/history**
- Query params: metric, period, granularity, tier, project
- Returns: Time-series data for charts

**GET /api/metrics/summary**
- Query params: period
- Returns: Performance summary table data

**GET /api/cost**
- Query params: period, breakdown_by (tier | project)
- Returns: Cost breakdown

### 12.6 Control Endpoints

**POST /api/supervisor/start**
- Action: Start supervisor

**POST /api/supervisor/stop**
- Action: Stop supervisor gracefully

**POST /api/supervisor/emergency_stop**
- Action: Force stop all workers and supervisor

**POST /api/workers/pause**
- Action: Pause all workers from claiming new tasks

**POST /api/workers/resume**
- Action: Resume workers

---

## 13. Responsive Design

### 13.1 Breakpoints

- **Mobile:** < 768px
- **Tablet:** 768px - 1024px
- **Desktop:** > 1024px

### 13.2 Mobile Optimizations

**Home Dashboard:**
- Stack hero metrics vertically (1 per row)
- Activity feed full width
- Quick stats accordion (collapsed by default)

**Task Queue:**
- Table becomes card list (1 task per card)
- Filters in slide-out drawer
- Swipe to reveal task actions

**Workers:**
- Grid becomes list (1 worker per row)
- Tap to expand worker details
- Bottom sheet for worker actions

**Projects:**
- List view only (no grid)
- Tap to open project detail page
- Form fields stack vertically

**Analytics:**
- Charts resize to fit width
- Tables horizontal scroll
- Date range picker optimized for touch

### 13.3 Touch Gestures

- **Swipe left** on task row → Show actions (Release, Retry, Delete)
- **Pull down** on activity feed → Refresh
- **Long press** on worker card → Show quick actions menu

---

## 14. Accessibility (WCAG 2.1 AA)

### 14.1 Requirements

**Keyboard Navigation:**
- All interactive elements accessible via Tab key
- Skip links for main content
- Keyboard shortcuts for common actions (configurable)

**Screen Reader Support:**
- Semantic HTML (header, nav, main, article)
- ARIA labels for icons and status badges
- Live regions for real-time updates (activity feed, metrics)
- Alt text for all images/charts

**Color Contrast:**
- Minimum 4.5:1 contrast for normal text
- Minimum 3:1 contrast for large text and UI components
- Do not rely on color alone (use icons + text)

**Focus Indicators:**
- Visible focus outline on all interactive elements
- High contrast focus indicator (2px blue border)

**Text Sizing:**
- Support browser zoom up to 200%
- Relative units (rem, em) for font sizes
- Minimum 16px base font size

**Reduced Motion:**
- Respect `prefers-reduced-motion` media query
- Option to disable animations

---

## 15. Performance Requirements

### 15.1 Load Times

- **Initial page load:** < 2 seconds (on 3G connection)
- **Subsequent navigation:** < 500ms (single-page app)
- **API response time:** < 200ms (99th percentile)

### 15.2 Real-Time Updates

- **Metrics refresh:** Every 5 seconds
- **Activity feed:** Every 2 seconds (or WebSocket)
- **Worker status:** Every 10 seconds
- **UI update latency:** < 100ms after data received

### 15.3 Optimization Strategies

**Frontend:**
- Code splitting (lazy load analytics, project management)
- Image optimization (WebP with JPEG fallback)
- Asset compression (Gzip/Brotli)
- Caching strategy (service worker for offline support)

**Backend:**
- API response caching (Redis)
- Database query optimization (indexes)
- Pagination for large lists
- WebSocket for real-time events (reduce polling)

---

## 16. Security Considerations

### 16.1 Authentication

**Requirement:** Users must authenticate before accessing dashboard

**Options:**
- GitHub OAuth (recommended - leverages existing GitHub access)
- Username/password with 2FA
- SSO integration (SAML, OIDC)

**UI:**
- Login page with GitHub OAuth button
- Session timeout after 1 hour of inactivity
- "Remember me" option (30-day session)

### 16.2 Authorization

**Role-Based Access Control (RBAC):**

**Admin Role:**
- Full access (view, edit, delete, control)
- Manage projects
- Start/stop supervisor
- Restart/stop workers

**Operator Role:**
- View all data
- Retry/release tasks
- Restart workers (but not stop)
- Cannot edit configuration

**Viewer Role:**
- Read-only access
- View metrics, tasks, workers
- Cannot perform actions

**UI:**
- Show/hide controls based on role
- Display "Access Denied" message if unauthorized action attempted

### 16.3 Data Protection

**Sensitive Data:**
- API keys, GitHub tokens NOT displayed in UI (only "••••••••" mask)
- Task descriptions may contain sensitive info (respect GitHub repo permissions)

**Audit Log:**
- Log all control actions (start/stop supervisor, restart worker, etc.)
- Show audit log in settings (Admin only)

---

## 17. Error Handling

### 17.1 API Error States

**Network Error (No Connection):**
- Show banner: "No connection to server. Retrying..."
- Retry with exponential backoff
- Show last successful data (stale but visible)

**Server Error (500):**
- Show toast: "Server error. Please try again."
- Log error details (show in console for debugging)
- Option to retry or contact support

**Not Found (404):**
- Show empty state: "Task not found" or "Worker not found"
- Suggest actions (go back, search, refresh)

**Unauthorized (401/403):**
- Redirect to login page
- Show message: "Session expired. Please log in again."

### 17.2 Empty States

**No Tasks in Queue:**
- Message: "No tasks in queue. Supervisor is monitoring for new issues."
- Icon: Empty box or checkmark
- Action: "View Projects" or "Refresh"

**No Workers Active:**
- Message: "No workers active. Start the supervisor to begin processing tasks."
- Icon: Robot icon (grayed out)
- Action: "Start Supervisor" button

**No Projects Configured:**
- Message: "No projects configured. Add a project to get started."
- Icon: Plus icon
- Action: "Add Project" button

---

## 18. Dark Mode Support

**Requirement:** Support both light and dark themes

**Theme Toggle:**
- Location: User menu (top right)
- Options: Light | Dark | Auto (follow system preference)
- Persist preference in localStorage

**Color Adjustments:**
- Dark background: #1A1A1A
- Dark surface: #2D2D2D
- Dark text: #E0E0E0
- Maintain same accent colors (adjust brightness if needed)
- Ensure WCAG AA contrast in both themes

---

## 19. Future Enhancements

### 19.1 Phase 2 Features (Not Included in Initial Release)

**Cost Optimization Dashboard:**
- Show cost savings from tier optimization
- Suggest tier adjustments based on performance data
- Alert when cost exceeds budget thresholds

**Multi-Region Support:**
- Deploy workers in multiple regions
- Show latency by region
- Route tasks to nearest worker

**Slack/Discord Integration:**
- Send alerts to Slack/Discord channels
- Slash commands for dashboard queries (e.g., `/orchestrator status`)

**Mobile App:**
- Native iOS/Android app for on-the-go monitoring
- Push notifications for critical alerts

**AI Insights:**
- Use AI to analyze patterns and suggest optimizations
- Predictive alerts (e.g., "Queue likely to back up in 2 hours")

### 19.2 Advanced Analytics

**Comparison Mode:**
- Compare metrics across projects
- Compare time periods (this week vs. last week)
- Show deltas and trends

**Custom Reports:**
- Build custom reports with drag-and-drop metrics
- Export to PDF or Excel
- Schedule automated reports (email daily/weekly)

**Anomaly Detection:**
- Automatically detect unusual patterns (sudden drop in success rate)
- Alert when anomalies detected

---

## 20. Glossary

**Term** | **Definition**
--- | ---
**Task** | A unit of work representing a GitHub Issue to be implemented by a worker
**Worker** | An autonomous AI agent that claims and completes tasks
**Tier** | Complexity-based routing category (Gemini Flash, Gemini Pro, Claude)
**Supervisor** | The orchestrator that polls GitHub, routes tasks, and monitors workers
**Queue** | The task queue holding pending, in-progress, and completed tasks
**PR** | Pull Request - created by worker after completing a task
**Status** | Task lifecycle state (Pending, Claimed, In Progress, Review, Complete, Failed)
**Heartbeat** | Periodic signal from worker indicating it's alive and healthy
**Failover** | Switching to alternate model/tier when quota exhausted or error occurs
**Acceptance Criteria** | Checklist items extracted from issue body defining "done"
**Complexity** | Classification of task difficulty (Simple, Medium, Complex)
**TDD** | Test-Driven Development - write tests before code

---

## 21. Design Deliverables Checklist

For designers building the dashboard, deliver:

- [ ] **Wireframes** - Low-fidelity layouts for all 5 primary views
- [ ] **High-Fidelity Mockups** - Desktop and mobile designs
- [ ] **Component Library** - Reusable UI components (buttons, cards, badges, etc.)
- [ ] **Style Guide** - Colors, typography, spacing, iconography
- [ ] **Interaction Specs** - Animations, transitions, hover states
- [ ] **Responsive Breakpoints** - Mobile, tablet, desktop variations
- [ ] **Dark Mode** - Light and dark theme variants
- [ ] **Accessibility Audit** - WCAG 2.1 AA compliance checklist
- [ ] **Asset Export** - SVG icons, images optimized for web
- [ ] **Design System Documentation** - Guidelines for developers

---

## 22. Developer Handoff Notes

**Technology Recommendations:**

**Frontend Framework:**
- React with TypeScript (recommended for maintainability)
- Or Vue 3 with TypeScript
- Or Svelte (for performance)

**UI Component Library:**
- Shadcn/ui (React) - Headless, accessible components
- Or Material-UI (React) - Comprehensive component library
- Or Ant Design (React) - Enterprise-grade components

**Charts Library:**
- Recharts (React) - Simple, composable charts
- Or Chart.js - Framework-agnostic
- Or D3.js - Maximum customization

**State Management:**
- React Query (for API data caching)
- Zustand (for global state)

**Real-Time:**
- Socket.IO (WebSocket) - For live updates
- Or Server-Sent Events (SSE) - Simpler alternative

**Styling:**
- Tailwind CSS (utility-first)
- Or CSS Modules (scoped styles)

**Backend API:**
- Go with Gorilla Mux or Fiber (REST)
- WebSocket support (gorilla/websocket)

---

## Appendix A: Example Screenshots (To Be Created)

1. **Home Dashboard** - Overview with hero metrics, activity feed, quick stats
2. **Task Queue - List View** - Table with filters, search, pagination
3. **Task Queue - Kanban View** (Alternative) - Columns by status
4. **Task Detail Modal** - Full task information, timeline, actions
5. **Worker Grid** - Cards showing worker status, health, stats
6. **Worker Detail** - Expanded view with tabs (overview, history, health, logs)
7. **Project List** - Cards showing project info and metrics
8. **Project Detail** - Configuration, metrics, recent PRs, failures
9. **Analytics - Cost Tracking** - Pie chart, table, line graph
10. **Analytics - Performance Trends** - Multiple charts showing historical data
11. **Mobile Views** - Responsive layouts for mobile devices
12. **Dark Mode** - Same views in dark theme

---

## Appendix B: User Flows

### B.1 New User Onboarding
1. User visits dashboard URL
2. Redirected to login (GitHub OAuth)
3. User authenticates with GitHub
4. Dashboard loads (shows empty state - no projects)
5. User clicks "Add Project"
6. User fills project form (repo, conventions path, patterns)
7. User clicks "Test Configuration" (validates GitHub access)
8. User clicks "Save & Enable"
9. Dashboard shows new project card
10. User clicks "Start Supervisor" (top right)
11. Supervisor begins polling issues
12. Tasks appear in queue
13. Workers claim and complete tasks
14. User sees activity in real-time

### B.2 Troubleshooting Failed Task
1. User sees alert: "High failure rate (50% in last hour)"
2. User clicks notification to view failed tasks
3. Task Queue view loads, filtered by status=failed
4. User clicks failed task row
5. Task Detail Modal opens
6. User reads error message: "Could not extract branch name"
7. User clicks "View Logs"
8. Logs open in modal (shows LLM response)
9. User identifies issue (LLM returned text, not work)
10. User closes modal
11. User clicks "Retry Task"
12. Task status changes to Pending
13. Worker re-claims and completes successfully
14. User sees success notification

### B.3 Monitoring Worker Health
1. User navigates to Workers view
2. User sees worker card with red status circle
3. Card shows: "No heartbeat for 8 minutes"
4. User clicks "View Details"
5. Worker Detail Modal opens
6. Health Metrics tab shows last heartbeat timestamp
7. Logs tab shows: "Worker hung at line 127"
8. User clicks "Restart Worker"
9. Confirmation modal: "Restart worker gemini-pro-2?"
10. User confirms
11. Worker restarts, status turns green
12. User sees success notification

---

**End of UI/UX Requirements Specification**

---

**Document Version:** 1.0
**Last Updated:** 2026-06-05
**Authors:** Claude (AI Agent) based on multi-agent orchestration system analysis
**Status:** Draft for Designer Review

For questions or clarifications, reference the system source code at:
`C:\Users\Owner\OneDrive\Documents\Builder\multi-agent-system`
