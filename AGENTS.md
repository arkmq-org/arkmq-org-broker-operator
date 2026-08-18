# ActiveMQ Artemis Operator - AI Assistant Configuration

## Project Overview

This is a Kubernetes operator for Apache Artemis, written in Go using
the controller-runtime framework.

## When Writing Code

Follow the `AI_documentation/contribution_guide.md`

**Discover Patterns**:

- Use `codebase_search` to find similar implementations
- Check existing tests for patterns
- Follow TDD approach

**Follow Patterns**:

- Controller pattern with reconciliation loop
- StatefulSet-based broker management
- Validation chain for CRs
- Status condition management

**Test Coverage**:

- Find similar test patterns
- Follow TDD approach
- Every feature must have tests

## Code Guidelines

1. **Naming**: Follow naming conventions (search codebase for patterns)
    - Resources: `{cr-name}-{resource-type}-{ordinal}`
    - Functions: Clear, descriptive names
    - Constants: ALL_CAPS with descriptive names

2. **Structure**: Follow controller pattern
    - Reconcile functions in `controllers/`
    - Resource generation in `pkg/resources/`
    - Utilities in `pkg/utils/`

3. **Validation**: Use validation chain pattern
    - Search for validate functions in controllers/activemqartemis_reconciler.go
    - Chain validators together
    - Return early on errors

4. **Status**: Update status conditions
    - Valid, Deployed, Ready, ConfigApplied
    - See api/v1beta1/activemqartemis_types.go for status definitions

5. When producing code:
    - ✓ always create tests for the functionalities you're adding
    - ✓ run the tests after implementing a functionality
    - ✓ always run E2E tests related to your addition
    - ✓ if required start minikube to run the E2E tests
    - ✗ DON'T assume that your code is functional until tests are executed and green

## Test-Driven Development (MANDATORY)

### CRITICAL: Follow Outside-In TDD - E2E First, Unit Tests During Implementation

**Outside-In TDD Workflow (MUST be followed for ALL code changes):**

1. **Write E2E Test FIRST** → Define acceptance criteria from user perspective
   - Write E2E test that describes the complete feature behavior
   - Test should interact with the operator as a real user would
   - Test will FAIL initially (red phase at acceptance level)
   - **PAUSE HERE** → Present E2E test to human for review

2. **Human Review Gate** → Wait for explicit approval before implementation
   - Human reviews feature specification and E2E test approach
   - Human may request changes to the E2E test
   - Only proceed to implementation after approval

3. **Implement Layer by Layer** → Write unit tests DURING implementation
   - For each component/function you implement:
     - Write unit test first (red phase at unit level)
     - Implement minimal code to pass the unit test (green phase)
     - Refactor while keeping tests green
   - Work from outside (controller) to inside (utilities)
   - Follow existing patterns discovered via codebase_search
   - Unit tests guide the design of individual components

4. **Verify E2E Test Passes** → Feature complete when E2E test is green
   - Run E2E test to verify end-to-end behavior
   - All unit tests should already be passing
   - E2E test validates that all components work together

5. **Final Refactor** → Clean up implementation while keeping all tests green
   - Improve code quality across all layers
   - Ensure both E2E and unit tests remain green

**Test Execution (happens throughout workflow):**

- **Setup Environment** → AUTOMATICALLY (for E2E tests):
  - Check if minikube/cluster is running
  - Start minikube if needed (don't ask permission)
  - Verify cert-manager installed and ready

- **Run Tests** → Execute tests AUTOMATICALLY:
  - E2E tests at start and end: `USE_EXISTING_CLUSTER=true go test -v
  ./controllers -ginkgo.focus="<pattern>" -timeout 10m`
  - Unit tests during implementation: `go test ./controllers -run <TestName>`

- **Fix Issues** → If tests fail, fix and re-run

- **Complete** → Only mark done when ALL tests pass (E2E + all unit tests)

## Unit Test Quality Standards (MANDATORY)

### Write Meaningful Tests - Not Just Coverage

#### CRITICAL: Avoid Over-Mocking

When writing unit tests, ensure they actually test real behavior:

1. **Meaningful Testing**:
   - ✓ Test real logic and business rules
   - ✓ Test actual code paths and error handling
   - ✓ Use real structs and minimal mocking
   - ✗ Don't create tests that only verify mock interactions

2. **When Mocking is Acceptable**:
   - External dependencies (Kubernetes API, databases, network calls)
   - Time-dependent operations
   - File system operations
   - Random number generation

3. **When to Request Human Feedback**:
   - **PAUSE and ASK** if you need to mock >3 interfaces/dependencies
   - **PAUSE and ASK** if mocks make up >50% of test code
   - **PAUSE and ASK** if you're testing trivial getters/setters only
   - **PAUSE and ASK** if the test doesn't verify actual business logic

4. **Red Flags** (request human review):
   - Tests that only verify mock method calls without assertions on behavior
   - Tests that require complex mock setup with no real logic testing
   - Tests that would pass regardless of implementation correctness
   - Integration tests disguised as unit tests

**Example of GOOD unit test:**

```go
// Tests real validation logic without mocking
func TestValidateBrokerConfig(t *testing.T) {
    config := &BrokerConfig{Size: -1}
    err := ValidateBrokerConfig(config)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "size must be positive")
}
```

**Example of BAD unit test (request human feedback):**

```go
// Only tests mock interactions, not real behavior
func TestProcessBroker(t *testing.T) {
    mockClient := &MockClient{}
    mockValidator := &MockValidator{}
    mockLogger := &MockLogger{}
    // ... 50 lines of mock setup ...
    mockClient.EXPECT().Get(gomock.Any()).Times(1)
    // No actual assertion on behavior or results
}
```

## Definition of Done

- ✅ E2E test written FIRST (defines acceptance criteria)
- ✅ E2E test reviewed and approved by human
- ✅ Unit tests written DURING implementation (for each component)
- ✅ Unit tests are meaningful (not just mock verification)
- ✅ Code compiles with no errors
- ✅ All unit tests passing
- ✅ E2E test passing (validates end-to-end behavior)
- ✅ Code refactored while maintaining green tests
- ✅ No trailing spaces in the generated code
- ❌ Code is NOT done if E2E test wasn't written first
- ❌ Code is NOT done if E2E test wasn't reviewed by human
- ❌ Code is NOT done if tests haven't run
- ❌ Code is NOT done if any tests are failing


## E2E Testing Reference

Notes:

- Automatically check cluster status before E2E tests
- Use minikube profile "aiprofile" to avoid interfering with existing clusters
- Automatically start minikube if needed (4GB RAM, 2 CPUs minimum)
- **CRITICAL**: Configure ingress with SSL passthrough BEFORE running
restricted mode tests
- Refer to contribution_guide.md (lines 850-889) for complete setup details
- Only ask user if setup fails
- Clean up test resources after test completion

### Quick Start - Running Specific E2E Tests

For quick iteration on specific features (recommended profile: `aiprofile`):

```bash
# 1. Start minikube with dedicated profile
minikube start --profile aiprofile --memory=4096 --cpus=2 \
  --extra-config=kubelet.sync-frequency=5s
minikube profile aiprofile

# 2. Configure ingress with SSL passthrough (CRITICAL for restricted mode tests)
minikube addons enable ingress
kubectl wait --namespace ingress-nginx --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller --timeout=120s
kubectl patch deployment ingress-nginx-controller -n ingress-nginx --type='json' \
  -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value":"--enable-ssl-passthrough"}]'
kubectl rollout status deployment/ingress-nginx-controller -n ingress-nginx

# 3. Run specific test (see "Running Specific Test Suites" section for more options)

# 4. Cleanup
minikube delete --profile aiprofile
```

**Note:** cert-manager will be auto-installed by tests if not present.

### Full E2E Test Suite Setup

For comprehensive testing before submitting PRs:

**1. Start Minikube**

```bash
# Start with adequate resources
minikube start --memory=8192 --cpus=4 \
  --extra-config=kubelet.sync-frequency=5s
```

**2. Configure Ingress with SSL Passthrough**

```bash
# Enable and configure ingress
minikube addons enable ingress
kubectl wait --namespace ingress-nginx --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller --timeout=120s

# Enable SSL passthrough (required for TLS proxy)
kubectl patch deployment ingress-nginx-controller -n ingress-nginx --type='json' \
  -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--enable-ssl-passthrough"}]'
kubectl rollout status deployment/ingress-nginx-controller -n ingress-nginx --timeout=120s

# Verify SSL passthrough
kubectl get deployment ingress-nginx-controller -n ingress-nginx \
  -o jsonpath='{.spec.template.spec.containers[0].args}' | grep "enable-ssl-passthrough"
```

**3. Install Development Tools**

```bash
make helm controller-gen envtest
```

**4. Generate and Install CRDs**

```bash
# Generate CRDs and code
make generate-deploy

# Install CRDs (using server-side apply for large CRDs)
make install

# Verify
kubectl get crds | grep broker.amq.io
```

**5. Run Test Suite**

```bash
# Run all tests (excludes deployed operator tests)
make test-mk-v

# Expected results:
# - ~213 tests pass (out of 226 total)
# - ~13 tests skipped (labeled 'do')
# - ~91% coverage
# - ~45-50 minutes duration
```

### Test Targets Reference

```bash
# Local operator mode (operator runs on host, excludes 'do' tests)
make test-mk          # Standard output
make test-mk-v        # Verbose output

# Deployed operator mode (operator in cluster, only 'do' tests)
make test-mk-do       # Standard output
make test-mk-do-v     # Verbose output

# Fast deployed operator mode (excludes 'slow' tests)
make test-mk-do-fast
make test-mk-do-fast-v
```

**Configuration:**
- `test-mk`: 120-min timeout, excludes `do` labeled tests
- `test-mk-do`: 60-min timeout, only `do` labeled tests
- `test-mk-do-fast`: 30-min timeout, `do && !slow` labeled tests

**Cert-Manager Auto-Installation:**
Tests automatically install cert-manager via Helm if not present and clean it up afterward. Pre-existing installations are left unchanged. See `controllers/common_util_test.go`.

### Running Specific Test Suites

```bash
# By label
USE_EXISTING_CLUSTER=true go test -v ./controllers \
  -ginkgo.label-filter="controller-cert-mgr-test" -timeout 120m

# By focus pattern
USE_EXISTING_CLUSTER=true go test -v ./controllers \
  -ginkgo.focus="minimal restricted rbac" -timeout 120m

# Skip specific tests
USE_EXISTING_CLUSTER=true go test -v ./controllers \
  -ginkgo.skip="operator role access" -ginkgo.label-filter='!do' -timeout 120m

# Specific test file
USE_EXISTING_CLUSTER=true go test -v ./controllers \
  -run TestAPIs -ginkgo.focus="control plane override" -timeout 30m
```

### Troubleshooting

**Tests timeout waiting for broker pods:**
```bash
kubectl get pods -n test -w
kubectl get events -n test --sort-by='.lastTimestamp'
kubectl describe pod <pod-name> -n test
```

**Connection refused to ingress:**
```bash
kubectl get pods -n ingress-nginx
kubectl get deployment ingress-nginx-controller -n ingress-nginx \
  -o jsonpath='{.spec.template.spec.containers[0].args}' | grep ssl-passthrough
kubectl get ingress -A
```

**CRD annotation size limit exceeded:**
```bash
kubectl apply -f config/crd/bases/broker.amq.io_activemqartemises.yaml --server-side
```

**Not enough resources:**
```bash
minikube delete
minikube start --cpus=4 --memory=8192
```

### Cleanup

```bash
# Delete test namespaces
kubectl delete namespace test other restricted --ignore-not-found=true

# Stop or delete minikube
minikube stop              # Preserves state
minikube delete            # Complete removal
minikube delete --profile aiprofile  # For aiprofile profile
```
