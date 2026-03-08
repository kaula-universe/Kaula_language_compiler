package codegen

import (
	"kaula-compiler/internal/test"
	"testing"
)

func TestCodegenBasic(t *testing.T) {
	testCases := []test.TestCase{
		{
			Name:  "Basic function",
			Input: "fn main() { println(42); }",
			Expected: "#include <stdint.h>\n#include <stdbool.h>\n#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\n#include \"../std/std.h\"\n\n\nint main() {\n    printf(\"%s\n\", 42);\n    return 0;\n}\n",
		},
		{
			Name:  "Variable declaration",
			Input: "int x = 42; println(x);",
			Expected: "#include <stdint.h>\n#include <stdbool.h>\n#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\n#include \"../std/std.h\"\n\n\nint main() {\n    int x = 42;\n    printf(\"%s\n\", x);\n    return 0;\n}\n",
		},
		{
			Name:  "Nullable variable",
			Input: "int? x = null; if (x != null) { println(*x); }",
			Expected: "#include <stdint.h>\n#include <stdbool.h>\n#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\n#include \"../std/std.h\"\n\n\nint main() {\n    int* x = NULL;\n    if (x != null) {\n        printf(\"%s\n\", *x);\n        if (x != NULL) { free(x); }\n    }\n    return 0;\n}\n",
		},
	}

	test.RunCodegenTest(t, testCases)
}
