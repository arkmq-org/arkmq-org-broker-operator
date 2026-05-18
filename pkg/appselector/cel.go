/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package appselector

import (
	"context"
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
	celtypes "github.com/google/cel-go/common/types"
	lru "github.com/hashicorp/golang-lru/v2"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DefaultExpression is the default CEL expression when none is specified.
	// It restricts apps to the same namespace as the service (secure by default).
	DefaultExpression = "app.metadata.namespace == service.metadata.namespace"
)

var (
	// celEnv is the CEL environment, created once at package initialization.
	// It defines the available variables (app, service, appNamespace, serviceNamespace) for CEL expressions.
	celEnv *cel.Env

	// celProgramCache caches compiled CEL programs with metadata by expression string.
	// This avoids recompiling the same expression on every evaluation.
	// Uses LRU cache with a maximum of 1000 entries to prevent unbounded memory growth.
	celProgramCache *lru.Cache[string, *celProgramMetadata]

	// hasNamespacePermission indicates whether the operator has permission to list/get namespaces.
	// Set once during initialization in main.go to avoid repeated failed API calls when permission is absent.
	hasNamespacePermission bool
)

// celProgramMetadata stores a compiled CEL program and tracks which variables it references.
type celProgramMetadata struct {
	program              cel.Program
	usesAppNamespace     bool
	usesServiceNamespace bool
}

// init initializes the CEL environment and program cache once at package load time.
func init() {
	var err error
	celEnv, err = cel.NewEnv(
		cel.Variable("app", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("service", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("appNamespace", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("serviceNamespace", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		// This should never happen with valid variable declarations
		panic(fmt.Sprintf("failed to create CEL environment: %v", err))
	}

	// Initialize LRU cache with 1000 entry limit
	celProgramCache, err = lru.New[string, *celProgramMetadata](1000)
	if err != nil {
		// This should never happen with valid size parameter
		panic(fmt.Sprintf("failed to create CEL program cache: %v", err))
	}
}

// SetNamespacePermission sets whether the operator has permission to access namespaces.
// This should be called once during operator initialization to avoid repeated failed API calls.
func SetNamespacePermission(hasPermission bool) {
	hasNamespacePermission = hasPermission
}

// ValidateExpression validates a CEL expression without evaluating it.
// This is used during BrokerService spec validation to provide immediate feedback.
// Returns nil if the expression is valid, error otherwise.
func ValidateExpression(expression string) error {
	if expression == "" {
		// Empty is valid (will use default)
		return nil
	}

	// Compile the expression to check syntax and return type
	ast, issues := celEnv.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return fmt.Errorf("failed to compile CEL expression: %w", issues.Err())
	}

	// Check that expression returns boolean
	if ast.OutputType() != cel.BoolType {
		return fmt.Errorf("CEL expression must return boolean, got %v", ast.OutputType())
	}

	return nil
}

// Matches checks if an app matches a service's appSelectorExpression.
// This is the main entry point for both BrokerApp and BrokerService controllers.
// Returns (matches, error). On error, matches will be false (fail-safe).
func Matches(app *broker.BrokerApp, service *broker.BrokerService, client client.Client) (bool, error) {
	expression := service.Spec.AppSelectorExpression
	if expression == "" {
		expression = DefaultExpression
	}

	return evaluateExpression(expression, app, service, client)
}

// analyzeVariableUsage walks the CEL AST to determine which variables are referenced.
// This allows us to skip fetching namespace objects when they're not used in the expression.
func analyzeVariableUsage(ast *cel.Ast) (usesAppNamespace, usesServiceNamespace bool) {
	walkExpr(ast.Expr(), &usesAppNamespace, &usesServiceNamespace)
	return
}

// walkExpr recursively walks the CEL expression tree to find variable references.
func walkExpr(expr *exprpb.Expr, usesAppNamespace, usesServiceNamespace *bool) {
	if expr == nil {
		return
	}

	switch expr.ExprKind.(type) {
	case *exprpb.Expr_IdentExpr:
		// Direct variable reference (e.g., "appNamespace", "serviceNamespace")
		ident := expr.GetIdentExpr()
		if ident.Name == "appNamespace" {
			*usesAppNamespace = true
		} else if ident.Name == "serviceNamespace" {
			*usesServiceNamespace = true
		}

	case *exprpb.Expr_SelectExpr:
		// Field selection (e.g., "appNamespace.metadata.name")
		sel := expr.GetSelectExpr()
		walkExpr(sel.Operand, usesAppNamespace, usesServiceNamespace)

	case *exprpb.Expr_CallExpr:
		// Function call (e.g., "has(appNamespace.metadata.labels)")
		call := expr.GetCallExpr()
		if call.Target != nil {
			walkExpr(call.Target, usesAppNamespace, usesServiceNamespace)
		}
		for _, arg := range call.Args {
			walkExpr(arg, usesAppNamespace, usesServiceNamespace)
		}

	case *exprpb.Expr_ListExpr:
		// List literal (e.g., "[appNamespace, serviceNamespace]")
		list := expr.GetListExpr()
		for _, elem := range list.Elements {
			walkExpr(elem, usesAppNamespace, usesServiceNamespace)
		}

	case *exprpb.Expr_StructExpr:
		// Struct/map literal (e.g., "{'key': appNamespace}")
		structExpr := expr.GetStructExpr()
		for _, entry := range structExpr.Entries {
			walkExpr(entry.GetMapKey(), usesAppNamespace, usesServiceNamespace)
			walkExpr(entry.GetValue(), usesAppNamespace, usesServiceNamespace)
		}

	case *exprpb.Expr_ComprehensionExpr:
		// List comprehension (e.g., "items.filter(i, i.ns == appNamespace)")
		comp := expr.GetComprehensionExpr()
		walkExpr(comp.IterRange, usesAppNamespace, usesServiceNamespace)
		walkExpr(comp.AccuInit, usesAppNamespace, usesServiceNamespace)
		walkExpr(comp.LoopCondition, usesAppNamespace, usesServiceNamespace)
		walkExpr(comp.LoopStep, usesAppNamespace, usesServiceNamespace)
		walkExpr(comp.Result, usesAppNamespace, usesServiceNamespace)
	}
}

// evaluateExpression evaluates a CEL expression to determine if an app matches.
// Returns true if the app matches, false otherwise.
// Uses a cache to avoid recompiling expressions on every evaluation.
func evaluateExpression(expression string, app *broker.BrokerApp, service *broker.BrokerService, client client.Client) (bool, error) {
	// Validate inputs
	if app == nil || service == nil || client == nil {
		return false, fmt.Errorf("app, service, and client must not be nil")
	}
	if expression == "" {
		return false, fmt.Errorf("expression cannot be empty")
	}

	// Get or compile the CEL program with metadata
	metadata, err := getOrCompileProgram(expression)
	if err != nil {
		return false, err
	}

	// Convert BrokerApp and BrokerService to unstructured maps for CEL
	appMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(app)
	if err != nil {
		return false, fmt.Errorf("failed to convert BrokerApp to unstructured: %w", err)
	}

	serviceMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(service)
	if err != nil {
		return false, fmt.Errorf("failed to convert BrokerService to unstructured: %w", err)
	}

	// Conditionally fetch namespace objects based on expression analysis.
	// Only fetch namespaces if the expression actually references them.
	// This optimization reduces unnecessary API calls and latency.
	var appNamespaceMap, serviceNamespaceMap map[string]interface{}

	if metadata.usesAppNamespace {
		appNamespaceMap, err = fetchNamespace(app.Namespace, client)
		if err != nil {
			// If namespace fetch fails (e.g., due to RBAC), use empty map
			// This allows expressions that check for namespace existence to still work
			appNamespaceMap = make(map[string]interface{})
		}
	}

	if metadata.usesServiceNamespace {
		serviceNamespaceMap, err = fetchNamespace(service.Namespace, client)
		if err != nil {
			// If namespace fetch fails (e.g., due to RBAC), use empty map
			serviceNamespaceMap = make(map[string]interface{})
		}
	}

	// Evaluate with actual values (or empty/nil maps if not needed or fetch failed)
	out, _, err := metadata.program.Eval(map[string]interface{}{
		"app":              appMap,
		"service":          serviceMap,
		"appNamespace":     appNamespaceMap,
		"serviceNamespace": serviceNamespaceMap,
	})
	if err != nil {
		return false, fmt.Errorf("failed to evaluate CEL expression: %w", err)
	}

	// Convert result to boolean
	result, ok := out.(celtypes.Bool)
	if !ok {
		return false, fmt.Errorf("CEL expression returned non-boolean type: %v", out.Type())
	}

	return bool(result), nil
}

// getOrCompileProgram gets a compiled CEL program from cache, or compiles and caches it.
// This provides significant performance improvement by avoiding recompilation.
// Uses an LRU cache with 1000 entry limit to prevent unbounded memory growth.
// Also analyzes the AST to determine which variables are referenced, allowing
// conditional namespace fetching during evaluation.
func getOrCompileProgram(expression string) (*celProgramMetadata, error) {
	// Check cache (LRU cache is thread-safe)
	if metadata, found := celProgramCache.Get(expression); found {
		return metadata, nil
	}

	// Compile the expression
	ast, issues := celEnv.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile CEL expression: %w", issues.Err())
	}

	// Check that expression returns boolean
	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("CEL expression must return boolean, got %v", ast.OutputType())
	}

	// Create program
	prg, err := celEnv.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL program: %w", err)
	}

	// Analyze AST to determine which variables are referenced
	usesAppNamespace, usesServiceNamespace := analyzeVariableUsage(ast)

	// Cache the compiled program with metadata
	metadata := &celProgramMetadata{
		program:              prg,
		usesAppNamespace:     usesAppNamespace,
		usesServiceNamespace: usesServiceNamespace,
	}
	celProgramCache.Add(expression, metadata)

	return metadata, nil
}

// fetchNamespace fetches a namespace object and converts it to a map for CEL evaluation.
// Returns error if namespace cannot be fetched (e.g., RBAC permissions, cache not synced).
// Callers should handle errors gracefully to allow CEL expressions that don't reference
// namespace metadata to still function when namespace access is unavailable.
func fetchNamespace(namespaceName string, client client.Client) (map[string]interface{}, error) {
	// Short-circuit if we know we don't have permission - avoids repeated failed API calls
	if !hasNamespacePermission {
		return nil, fmt.Errorf("operator lacks permission to access namespaces")
	}

	var ns corev1.Namespace

	nsKey := types.NamespacedName{Name: namespaceName}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Get(ctx, nsKey, &ns); err != nil {
		// Return error if namespace cannot be fetched.
		// This could be due to:
		// - RBAC: operator lacks permission to read Namespace resources
		// - Cache not synced: informer hasn't started or synced yet
		// - Race condition: namespace being deleted while app/service still exists
		// Caller decides whether to fail-closed or continue with empty namespace map.
		return nil, fmt.Errorf("failed to fetch namespace %s: %w", namespaceName, err)
	}

	// Convert to unstructured map
	unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&ns)
	if err != nil {
		return nil, fmt.Errorf("failed to convert Namespace to unstructured: %w", err)
	}
	return unstructuredMap, nil
}
