"""
SOR (Sub-Ownership Release) 模型形式化验证
与 kaula-compiler/internal/sor/ 源码中的显式检查一一对应。
"""

from z3 import *


def main():
    print("=" * 64)
    print("  SOR Model Verification")
    print("=" * 64)

    State = Datatype("State")
    State.declare("Owned")
    State.declare("Released")
    State.declare("Moved")
    State.declare("Hollow")
    State.declare("Extracted")
    State.declare("UnionReleased")
    State = State.create()

    s = Const("s", State)

    may_read = Function("may_read", State, BoolSort())
    may_write = Function("may_write", State, BoolSort())
    may_yield_src = Function("may_yield_src", State, BoolSort())
    may_release_src = Function("may_release_src", State, BoolSort())
    may_extract_src = Function("may_extract_src", State, BoolSort())
    may_urelease_src = Function("may_urelease_src", State, BoolSort())

    post_yield_src = Function("post_yield_src", State, State)
    post_release_src = Function("post_release_src", State, State)
    post_urelease_src = Function("post_urelease_src", State, State)

    results = []

    def solver():
        m = Solver()
        m.set(timeout=10000)

        # CheckAccess(obj, AccessRead) 的通行规则
        # 源码: 仅 Moved/Hollow/Extracted 被拦截（ErrUseAfterMove/ErrNullDereference）
        # Owned/Released/UnionReleased 均可读
        m.add(ForAll([s], may_read(s) ==
            And(s != State.Moved, s != State.Hollow, s != State.Extracted)))

        # CheckAccess(obj, AccessWrite) 的通行规则
        # 源码: Released → ErrWriteOnReleased; Moved/Hollow/Extracted → ErrUseAfterMove/ErrNullDereference
        # Owned/UnionReleased 均可写
        m.add(ForAll([s], may_write(s) ==
            And(s != State.Released, s != State.Moved, s != State.Hollow, s != State.Extracted)))

        # Yield() 前置条件: CheckAccess(src, AccessTake) 要求 src == StateOwned
        m.add(ForAll([s], may_yield_src(s) == (s == State.Owned)))
        # Yield() 后置: src.State = StateMoved
        m.add(ForAll([s], Implies(may_yield_src(s), post_yield_src(s) == State.Moved)))

        # Release() 前置条件: src.State == StateOwned || src.State == StateReleased
        m.add(ForAll([s], may_release_src(s) == Or(s == State.Owned, s == State.Released)))
        # Release() 后置: src.State = StateReleased
        m.add(ForAll([s], Implies(may_release_src(s), post_release_src(s) == State.Released)))

        # Extract() 前置条件: src.State == StateOwned
        m.add(ForAll([s], may_extract_src(s) == (s == State.Owned)))
        # Extract() 不改変 source 自身的状态（仅 child position → Hollow）

        # execUnionRelease(): 先调用 Release(), 再设 src.State = StateUnionReleased
        m.add(ForAll([s], may_urelease_src(s) == (s == State.Owned)))
        m.add(ForAll([s], Implies(may_urelease_src(s), post_urelease_src(s) == State.UnionReleased)))

        return m

    def verify(prop, label):
        m = solver()
        m.add(Not(prop))
        r = m.check()
        ok = r == unsat
        results.append((label, ok))
        tag = "[SAFE]" if ok else ("[GAP]" if r == sat else f"[?] {r}")
        print(f"  {label:45s} {tag}")
        return ok

    # ================================================================
    # 权限矩阵 (ownership.go CheckAccess + CanWrite/CanYield)
    # ================================================================
    print("\n--- Permission Matrix ---")

    # CheckAccess 对 Owned 无拦截
    verify(
        ForAll([s], Implies(s == State.Owned,
            And(may_read(s), may_write(s), may_yield_src(s), may_release_src(s),
                may_extract_src(s), may_urelease_src(s)))),
        "Owned: all ops")

    # CheckAccess: Released 对 Write → ErrWriteOnReleased
    #             Yield/Extract 无对应权限
    verify(
        ForAll([s], Implies(s == State.Released,
            And(may_read(s), Not(may_write(s)), Not(may_yield_src(s)),
                may_release_src(s), Not(may_extract_src(s)), Not(may_urelease_src(s))))),
        "Released: R + L only")

    # CheckAccess: Moved → ErrUseAfterMove, Hollow/Extracted → ErrNullDereference
    for nm in ["Moved", "Hollow", "Extracted"]:
        sv = getattr(State, nm)
        verify(
            ForAll([s], Implies(s == sv,
                And(Not(may_read(s)), Not(may_write(s)), Not(may_yield_src(s)),
                    Not(may_release_src(s)), Not(may_extract_src(s)), Not(may_urelease_src(s))))),
            f"{nm}: all denied")

    # CheckAccess: UnionReleased 无拦截（仅 elected writer 可写，编译期约束）
    verify(
        ForAll([s], Implies(s == State.UnionReleased,
            And(may_read(s), may_write(s), Not(may_yield_src(s)),
                Not(may_release_src(s)), Not(may_extract_src(s)), Not(may_urelease_src(s))))),
        "UnionReleased: R + W")

    # ================================================================
    # 转移后置条件 (ownership.go Yield/Release)
    # ================================================================
    print("\n--- Transition Postconditions ---")

    verify(
        ForAll([s], Implies(may_yield_src(s), post_yield_src(s) == State.Moved)),
        "yield -> Moved")

    verify(
        ForAll([s], Implies(may_release_src(s), post_release_src(s) == State.Released)),
        "release -> Released")

    verify(
        ForAll([s], Implies(may_urelease_src(s), post_urelease_src(s) == State.UnionReleased)),
        "urelease -> UnionReleased")

    # ================================================================
    # 错误模式 (ownership.go CheckAccess)
    # ================================================================
    print("\n--- Error Patterns ---")

    verify(
        ForAll([s], Implies(s == State.Moved,
            Not(Or(may_read(s), may_write(s), may_yield_src(s), may_release_src(s),
                    may_extract_src(s), may_urelease_src(s))))),
        "No use-after-move")

    verify(
        ForAll([s], Implies(s == State.Released, Not(may_write(s)))),
        "No write-on-released")

    verify(
        ForAll([s], Implies(s == State.Hollow, Not(may_read(s)))),
        "No hollow-read (null deref)")

    verify(
        ForAll([s], Implies(s == State.Extracted,
            Not(Or(may_read(s), may_write(s), may_yield_src(s), may_release_src(s),
                    may_extract_src(s), may_urelease_src(s))))),
        "No access to Extracted")

    # ================================================================
    # 幂等性 (ownership.go Yield/Release 前置条件)
    # ================================================================
    print("\n--- Idempotency ---")

    # Yield 要求 src == Owned → yield 后为 Moved → 不可再 yield
    verify(
        ForAll([s], Implies(may_yield_src(s), Not(may_yield_src(post_yield_src(s))))),
        "No double-yield")

    # Release 接受 Owned || Released → 连续 release 合法
    verify(
        ForAll([s], Implies(s == State.Released, may_release_src(s))),
        "Release idempotent")

    # ================================================================
    total = len(results)
    passed = sum(1 for _, ok in results if ok)

    print(f"\n{'=' * 64}")
    print(f"  Summary: {passed}/{total} passed")
    if passed == total:
        print("  [PASS] All SOR properties verified against source.")
    else:
        for label, ok in results:
            if not ok:
                print(f"  [FAIL] {label}")
        print("  [FAIL] Some properties not entailed.")
    print("=" * 64)


if __name__ == "__main__":
    main()
