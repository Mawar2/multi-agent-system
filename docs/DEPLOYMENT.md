# Deployment Guide - Kaimi

This document explains the GCP deployment pipeline for Kaimi.

## Current Status: Phase 0

**Deployment Status:** Configured but not active

The CI/CD pipeline includes a deployment job that is **ready but waiting** for Phase 1. The job runs as a placeholder in Phase 0, displaying status information without deploying anything.

---

## Deployment Architecture

### Phase 0 (Current)
- ✅ CI/CD pipeline configured
- ✅ Deployment job structure in place
- ✅ Manual approval required via GitHub environment
- ⏸️ No actual deployment (placeholder only)

### Phase 1 (Future)
When Phase 1 begins, the deployment will:
1. Build Hunter Docker image
2. Push to Google Container Registry (GCR)
3. Deploy to Cloud Run
4. Configure Cloud Scheduler for daily runs

---

## Setting Up Manual Deployment Approval

The deployment job requires manual approval via a GitHub environment. Here's how to set it up:

### 1. Create the "production" Environment

**GitHub Repository → Settings → Environments → New environment**

1. **Name:** `production`
2. Click **"Configure environment"**

### 2. Configure Environment Protection Rules

Add these protection rules:

#### Required Reviewers
- ✅ Check **"Required reviewers"**
- Add reviewers:
  - `malik@bluemetatech.com` (you)
  - `thaithimmy2003@gmail.com` (developer)
- At least 1 approval required before deployment

#### Deployment Branches
- ✅ Select **"Selected branches"**
- Add branch: `main`
- This ensures only main branch can deploy

#### Wait Timer (Optional)
- Set a wait timer if you want a delay before deployment
- Recommended: 0 minutes (manual approval is the gate)

### 3. Environment Variables (Optional)

You can add environment-specific variables here, but we're using repository secrets which work fine.

### 4. Save Environment

Click **"Save protection rules"**

---

## How Manual Deployment Works

### Trigger

Deployment job runs when:
- ✅ Code is pushed to `main` branch
- ✅ All checks pass (tests, lint, GCP verification)
- ✅ `all-checks-pass` job succeeds

### Approval Flow

1. **CI runs automatically** on push to main
2. **All checks must pass** first
3. **Deployment job waits** for manual approval
4. **Notification sent** to required reviewers
5. **Reviewer approves** deployment in GitHub Actions UI
6. **Deployment executes** after approval

### Reviewing a Deployment

When deployment is triggered:

1. Go to **Actions** tab
2. Click on the workflow run
3. You'll see: **"Deploy to GCP - Waiting for approval"**
4. Click **"Review deployments"**
5. Select **production** environment
6. Add a comment (optional): "Approved for Phase 1 launch"
7. Click **"Approve and deploy"**

---

## What Gets Deployed (Phase 1)

### Hunter Agent to Cloud Run

**Service Name:** `hunter`
**Region:** `us-east4`
**Platform:** Cloud Run (managed)

**Configuration:**
- Memory: 512Mi
- CPU: 1
- Timeout: 300s (5 minutes)
- Max instances: 10
- Min instances: 0 (scales to zero when not used)

**Environment Variables:**
- `GCP_PROJECT_ID` - Project ID
- `GCP_REGION` - Deployment region
- `SAMGOV_API_KEY` - From Secret Manager

**Secrets:**
- `samgov-api-key` - Mounted from Secret Manager

### Cloud Scheduler

**Job Name:** `hunter-daily`
**Schedule:** `0 9 * * *` (9 AM daily, UTC)
**Target:** Cloud Run Hunter service
**Method:** POST to `/run` endpoint
**Auth:** Service account OIDC token

---

## Phase 0: Current Deployment Job

The deployment job currently runs a **placeholder script** that displays:

```
=========================================
GCP Deployment - Phase 0
=========================================

This job is configured but not yet deploying.
Phase 0 has no cloud infrastructure to deploy.

When Phase 1 begins, uncomment the steps below to:
  1. Build Hunter Docker image
  2. Push to Google Container Registry
  3. Deploy to Cloud Run
  4. Configure Cloud Scheduler for daily runs

Project: kaimi-seeker
Region: us-east4
Status: Ready for Phase 1 deployment
=========================================
```

This confirms the pipeline is working and ready for Phase 1.

---

## Activating Deployment for Phase 1

When Phase 1 work begins:

### 1. Uncomment Deployment Steps

In `.github/workflows/ci.yml`, uncomment these sections:

```yaml
# - name: Build Hunter Docker image
# - name: Push to Google Container Registry
# - name: Deploy Hunter to Cloud Run
# - name: Get Cloud Run URL
# - name: Configure Cloud Scheduler
```

Remove the `#` at the start of each line.

### 2. Enable Required APIs

```bash
# Artifact Registry (for Docker images)
gcloud services enable artifactregistry.googleapis.com

# Cloud Run
gcloud services enable run.googleapis.com

# Cloud Scheduler
gcloud services enable cloudscheduler.googleapis.com
```

### 3. Create Artifact Registry Repository

```bash
gcloud artifacts repositories create kaimi \
  --repository-format=docker \
  --location=us-east4 \
  --description="Kaimi Docker images"
```

### 4. Grant Additional IAM Roles

The service account needs permissions to deploy:

```bash
# Cloud Run Admin (deploy services)
gcloud projects add-iam-policy-binding kaimi-seeker \
  --member="serviceAccount:kaimi-dev@kaimi-seeker.iam.gserviceaccount.com" \
  --role="roles/run.admin"

# Service Account User (act as service account)
gcloud projects add-iam-policy-binding kaimi-seeker \
  --member="serviceAccount:kaimi-dev@kaimi-seeker.iam.gserviceaccount.com" \
  --role="roles/iam.serviceAccountUser"

# Artifact Registry Writer (push images)
gcloud projects add-iam-policy-binding kaimi-seeker \
  --member="serviceAccount:kaimi-dev@kaimi-seeker.iam.gserviceaccount.com" \
  --role="roles/artifactregistry.writer"

# Cloud Scheduler Admin (create jobs)
gcloud projects add-iam-policy-binding kaimi-seeker \
  --member="serviceAccount:kaimi-dev@kaimi-seeker.iam.gserviceaccount.com" \
  --role="roles/cloudscheduler.admin"
```

### 5. Test Deployment

1. Merge a small change to main
2. Watch CI/CD pipeline run
3. Approve deployment when prompted
4. Verify Hunter is deployed to Cloud Run
5. Check Cloud Scheduler job is created

---

## Deployment URLs

After Phase 1 deployment:

- **Cloud Run Service:** https://hunter-[hash]-ue.a.run.app
- **GCP Console:** https://console.cloud.google.com/run?project=kaimi-seeker
- **Cloud Scheduler:** https://console.cloud.google.com/cloudscheduler?project=kaimi-seeker

---

## Rollback Procedure

If deployment fails or has issues:

### Option 1: Redeploy Previous Version

```bash
# List revisions
gcloud run revisions list --service=hunter --region=us-east4

# Rollback to previous revision
gcloud run services update-traffic hunter \
  --to-revisions=hunter-00002-abc=100 \
  --region=us-east4
```

### Option 2: Revert Git Commit

```bash
# Revert the commit that caused issues
git revert <commit-hash>
git push origin main

# CI will redeploy the reverted version
```

---

## Monitoring Deployments

### View Logs

```bash
# Cloud Run logs
gcloud run services logs read hunter --region=us-east4

# Cloud Scheduler logs
gcloud scheduler jobs describe hunter-daily --location=us-east4
```

### Cloud Console

- **Cloud Run Logs:** https://console.cloud.google.com/run/detail/us-east4/hunter/logs
- **Cloud Scheduler Logs:** https://console.cloud.google.com/cloudscheduler

---

## Cost Monitoring

### Expected Costs (Phase 1)

| Service | Cost Model | Estimated |
|---------|------------|-----------|
| Cloud Run | Pay-per-use (requests + CPU time) | ~$5-10/month |
| Cloud Scheduler | $0.10/job/month | ~$0.10/month |
| Artifact Registry | $0.10/GB/month storage | ~$0.50/month |
| **Total** | | **~$5-11/month** |

Cloud Run scales to zero when not used, so costs are minimal during development.

---

## Security Best Practices

### Service Account Permissions
- ✅ Use dedicated service account for Cloud Run
- ✅ Grant minimal permissions (least privilege)
- ✅ Use Secret Manager for sensitive data
- ✅ Enable VPC connector if accessing internal resources

### Cloud Run Configuration
- ✅ Disable unauthenticated access after testing
- ✅ Use OIDC tokens for Cloud Scheduler
- ✅ Set resource limits (memory, CPU)
- ✅ Enable request logging

### Docker Image Security
- ✅ Use minimal base images (alpine)
- ✅ Don't include secrets in image
- ✅ Scan images for vulnerabilities
- ✅ Use specific version tags, not `latest` in production

---

## Troubleshooting

### Deployment Fails: "Permission denied"

**Issue:** Service account missing Cloud Run permissions

**Fix:**
```bash
gcloud projects add-iam-policy-binding kaimi-seeker \
  --member="serviceAccount:kaimi-dev@kaimi-seeker.iam.gserviceaccount.com" \
  --role="roles/run.admin"
```

### Deployment Fails: "Artifact Registry not found"

**Issue:** Artifact Registry repository not created

**Fix:**
```bash
gcloud artifacts repositories create kaimi \
  --repository-format=docker \
  --location=us-east4
```

### Cloud Scheduler Job Fails

**Issue:** Cloud Run service not allowing unauthenticated requests

**Fix:**
```bash
# Allow Cloud Scheduler to invoke
gcloud run services add-iam-policy-binding hunter \
  --member="serviceAccount:kaimi-dev@kaimi-seeker.iam.gserviceaccount.com" \
  --role="roles/run.invoker" \
  --region=us-east4
```

---

## Next Steps

### Phase 0 (Now)
- ✅ Set up "production" environment in GitHub
- ✅ Test manual approval flow with placeholder deployment
- ✅ Verify all checks pass before deployment is triggered

### Phase 1 (When ready)
- [ ] Uncomment deployment steps in CI/CD
- [ ] Enable Cloud Run and Artifact Registry APIs
- [ ] Create Artifact Registry repository
- [ ] Grant additional IAM permissions
- [ ] Test actual deployment to Cloud Run
- [ ] Configure Cloud Scheduler for daily runs
- [ ] Monitor costs and performance

---

## Questions?

- **CI/CD Issues:** Check [.github/workflows/ci.yml](.github/workflows/ci.yml)
- **GCP Setup:** See [docs/GCP_SETUP.md](./GCP_SETUP.md)
- **Architecture:** See [ARCHITECTURE.md](../ARCHITECTURE.md)
- **Workflow:** See [WORKFLOW.md](../WORKFLOW.md)

**Deployment is ready for Phase 1! 🚀**
