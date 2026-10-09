# A16 Java high-memory optimization

## Heap values

| Tool | Current A16 value | Result |
| --- | --- | --- |
| Javac | `4096M` | `6144M` |
| ErrorProne | `8192M` | `10240M` |
| D8 | `-JXmx4096M` | `-JXmx6144M` |
| R8 | `-JXmx4096M` | `-JXmx6144M` |
| Metalava | `-J-Xmx6114m` | `-J-Xmx12288m` |

Javac and ErrorProne heap flags interpolate their corresponding size variables.
`D8Flags` and `R8Flags` carry the Xmx values directly. `metalavaCmd()` adds
`JavacVmFlags`, `MetalavaVmFlags` and `MetalavaAddOpens` before the explicit
Metalava maximum heap flag.

## A16 source differences

The current A16 source uses `-J-Xmx6114m` in `metalavaCmd()`. The historical
patch also changed indentation, comments and method-chain layout; those changes
were omitted. Only heap values were changed. Existing Javac VM flags, Metalava
flags, bootclasspath/classpath handling, response-file input handling and D8/R8
flags were preserved.

## Files changed

- `java/config/config.go`
- `java/droidstubs.go`
- `.codex-reference/a16-java-high-memory-optimization.md` (this handoff)

The focused source diff contains only five heap-value edits. No other source
files were changed as part of this task. `git diff --check` passed.

## Recommended combined commit

Subject:

```text
java: optimize build tool heap sizes for high-memory systems
```

Body:

```text
Increase the JVM heap available to Javac, ErrorProne, D8, R8 and Metalava
for high-memory build hosts.

Raise Javac, D8 and R8 to 6144M, ErrorProne to 10240M and Metalava to
12288M to reduce memory pressure and avoid OOM failures during
resource-intensive Java and API generation workloads.
```
