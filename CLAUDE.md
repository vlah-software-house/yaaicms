# **AI Assistant Instructions & Project Context**

This file contains the core project specifications, technical stack, and strict workflow rules for any AI coding assistant or tool operating in this repository.  
**CRITICAL: You must read and adhere to these instructions before executing any commands, generating code, or modifying files.** As the project evolves, it is your responsibility to update this CLAUDE.md file to reflect new architectural decisions or workflow changes.

## **1\. Tech Stack**

* **Backend:** Go (Golang)  
* **Frontend:** HTMX, AlpineJS  
* **Database:** PostgreSQL  
* **Cache:** Valkey  
* **Testing:** Go standard testing (Unit/Functional), Playwright (E2E)  
* **Infrastructure:** Docker, Kubernetes

## **2\. Git & Development Workflow**

Follow this exact sequence for every new task, feature, fix, or patch:

### **Phase A: Planning & Branching**

1. **Confirm the Plan:** Before writing code, outline the plan and await user confirmation.  
2. **Branch Check:** Check the current branch. If it is NOT main, prompt the user to merge any outstanding changes into main and checkout main. Do not proceed until on main.  
3. **New Branch:** Once on main and the plan is confirmed, create and checkout a new, suggestively named branch (e.g., feat/add-user-auth, fix/cache-invalidation).  
4. **Stay on Branch:** Continue all work for the confirmed plan on this new branch until the user explicitly confirms the feature, fix, or patch is 100% complete.

### **Phase B: Step-by-Step Execution & Logging**

1. **Log Finished Steps:** For every confirmed finished step within the task, create a log entry file in the work/logs/ directory. Use a descriptive filename (e.g., work/logs/20260224\_implemented\_valkey\_connection.md).  
2. **Commit:** Immediately after writing the log file, commit the changes to the current branch with a clear, descriptive commit message.

### **Phase C: Completion & PR**

1. **Final Review:** Wait for the user to confirm the overall task is complete.
2. **Push & PR:** Once confirmed, push the branch to the remote repository and create a Pull Request (or Merge Request).

### **Phase D: Mandatory PR Review Before Merge**

1. **Never merge directly.** Every change MUST go through a Pull Request — no exceptions.
2. **Wait for automated reviews:** After creating the PR, wait for **Qodana** (code quality) and **Gemini Code Assist** (security/review) to post their results.
3. **Fix issues first:** If Qodana or Gemini flag problems, fix them on the branch and push before merging.
4. **Merge only when clean:** Only merge after all automated checks pass and flagged issues are resolved or acknowledged by the user.

## **3\. Go Coding Standards**

* **Version Check:** ALWAYS check the available Go version on the host machine (go version) before starting any code generation to ensure absolute compatibility.  
* **Best Practices:** Strictly adhere to idiomatic Go best practices, standard project layouts, and effective concurrency patterns.  
* **Readability:** All code must be heavily commented, clean, and easily readable by human developers. Document exported functions, structs, and complex logic blocks.

## **4\. Testing Requirements**

* **Coverage:** Implement tests everywhere. Aim for near 100% coverage.  
* **Unit & Functional:** Write comprehensive unit tests and functional tests for all Go backend logic.  
* **End-to-End (E2E):** Write E2E tests based on specification descriptions and user journeys in the interface. **You must use Playwright** for testing the HTMX/AlpineJS web pages.

## **5\. Infrastructure & DevOps**

* **Docker:** Use Docker for all local services (PostgreSQL, Valkey). Ensure a fully working Dockerfile and pipeline exist for building the application's main container.  
* **Kubernetes (K8s):**  
  * Deploy to the testing environment using Kubernetes manifests.  
  * Keep manifests updated, modular, and customizable for multiple environments (e.g., using Kustomize or Helm if appropriate, or structured manifest directories).  
  * Environment uses K3S with Cert-Manager and Traefik Ingress, with current new Traefik CRDs. Do not set any file saves in containers, and do not create volumes.
* **Seeding Data:** Write and maintain database seeding scripts/manifests based on the application specifications to ensure a populated testing environment.  
* **Secrets Management:** Two environment files exist, both gitignored:
  * **`.secrets`** — Testing/staging environment credentials for Kubernetes deployment and human QA. Contains DB credentials for the remote testing cluster, Valkey credentials, testing URL, and AI API keys. **Always ask the user for any missing variables** before deploying. Do not hardcode secrets.
  * **`.env`** — Local development configuration. The AI agent may freely create and manage this file with whatever credentials are needed for local Docker services (PostgreSQL, Valkey). AI provider keys may be copied from `.secrets` for local use since they are service-level keys, not environment-specific.
* **Database Management:** Use the postgres user password to check existence of defined database name, and user that must be its owner. Managed database host, postgres user password, user and password should be availalbe in .secrets`
* **Media files** Will work with S3 compatible object store (most cases CEPH based).

## **6\. AI Provider Integration**

YaaiCMS supports **four AI providers** for content generation and template design. All providers follow a common interface and can be switched at runtime from the admin Settings page.

### Supported Providers

| Provider | Key env var | Default model | Base URL env var (optional) |
|---|---|---|---|
| OpenAI | `OPENAI_API_KEY` | `gpt-4o` | `OPENAI_BASE_URL` |
| Google Gemini | `GEMINI_API_KEY` | `gemini-3.1-pro-preview` | `GEMINI_BASE_URL` |
| Anthropic Claude | `CLAUDE_API_KEY` | `claude-sonnet-4-6` | `CLAUDE_BASE_URL` |
| Mistral | `MISTRAL_API_KEY` | `mistral-large-latest` | `MISTRAL_BASE_URL` |

### Architecture

* **`AI_PROVIDER`** env var sets the default active provider on startup.
* Each provider has its own `*_API_KEY`, `*_MODEL`, and optional `*_BASE_URL` variables.
* Base URLs default to the official API endpoints — only override for proxies or self-hosted instances.
* Runtime switching: the active provider and model are stored in the database (settings table) and override the env var default. The admin Settings page lets users switch without restarting the server.
* Only providers with a valid API key configured are selectable in the UI.
* Code architecture: an `ai.Provider` interface with `Generate(prompt string) (string, error)` — each provider implements it. A registry/factory selects the provider by name.

## **7\. AI Tool Coordination & Delegation**

* **Task Management:** Maintain a list of ongoing, pending, and completed tasks inside the work/tasks/ directory.  
* **Grouped Files:** Group tasks logically into files (e.g., work/tasks/frontend.md, work/tasks/database.md).  
* **Delegation:** Format these task files clearly so that *other* AI coding tools or agents can read them, understand the current project state, and pick up delegated work seamlessly.
