# BENCH:PREFIX
import math
# BENCH:BEGIN
def has_close_elements(numbers, threshold):
    for i, a in enumerate(numbers):
        for j, b in enumerate(numbers):
            if i != j and abs(a - b) < threshold:
                return True
    return False

# (imports/types counted above)

# BENCH:END

# BENCH:MAIN:BEGIN
if __name__ == "__main__":
    assert has_close_elements([1.0, 2.0, 3.9, 4.0, 5.0, 2.2], 0.3) == True
    assert has_close_elements([1.0, 2.0, 3.9, 4.0, 5.0, 2.2], 0.05) == False
    assert has_close_elements([1.0, 2.0, 5.9, 4.0, 5.0], 0.8) == False
    assert has_close_elements([1.1, 2.2, 3.1, 4.1, 5.1], 1.0) == True
    print("ok")
# BENCH:MAIN:END
