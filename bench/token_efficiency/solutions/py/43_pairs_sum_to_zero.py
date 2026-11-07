# BENCH:BEGIN
def pairs_sum_to_zero(l):
    for i, a in enumerate(l):
        for b in l[i + 1:]:
            if a + b == 0:
                return True
    return False

# BENCH:END

# BENCH:MAIN:BEGIN
if __name__ == "__main__":
    assert pairs_sum_to_zero([1, 3, 5, 0]) == False
    assert pairs_sum_to_zero([2, 4, -5, 3, 5, 7]) == True
    assert pairs_sum_to_zero([1]) == False
    print("ok")
# BENCH:MAIN:END
