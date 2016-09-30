package patch

import (
	"fmt"
	"testing"
)

func TestChangedLines(t *testing.T) {
	p := `@@ -114,6 +114,7 @@ func convertToNQuad(ctx context.Context, mutation string) ([]rdf.NQuad, error) {
           var nquads []rdf.NQuad
           r := strings.NewReader(mutation)
           scanner := bufio.NewScanner(r)
+          x.Trace(ctx, "Converting to NQuad")

           // Scanning the mutation string, one line at a time.
           for scanner.Scan() {
@@ -178,21 +179,11 @@ func convertToEdges(ctx context.Context, nquads []rdf.NQuad) (mutationResult, er
 }

 func applyMutations(ctx context.Context, m worker.Mutations) error {
-          left, err := worker.MutateOverNetwork(ctx, m)
+          err := worker.MutateOverNetwork(ctx, m)
           if err != nil {
               x.TraceError(ctx, x.Wrapf(err, "Error while MutateOverNetwork"))
               return err
           }
-          if len(left.Set) > 0 || len(left.Del) > 0 {
-              x.TraceError(ctx, x.Errorf("%d edges couldn't be applied", len(left.Del)+len(left.Set)))
-              for _, e := range left.Set {
-                  x.TraceError(ctx, x.Errorf("Unable to apply set mutation for edge: %v", e))
-              }
-              for _, e := range left.Del {
-                  x.TraceError(ctx, x.Errorf("Unable to apply delete mutation for edge: %v", e))
-              }
-              return x.Errorf("Unapplied mutations")
-          }
           return nil
 }`

	m := ChangedLines(p)
	if len(m) != 2 {
		t.Fatalf("Expected: %v changed lines. Got: %v", 2, len(m))
	}
	if m[117] != 4 {
		t.Fatalf("Expected value to be : %v. Got: %v", 2, m[117])
	}
	if m[182] != 13 {
		t.Fatalf("Expected value to be : %v. Got: %v", 13, m[182])
	}

	p = `@@ -0,0 +1,9 @@
+package dummy
+
+type ErrID struct {
+          a int
+}
+
+type errId struct {
+          b int
+}`
	fmt.Println(ChangedLines(p))
}
