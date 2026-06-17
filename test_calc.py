import unittest
from calc import add, sub, mul, div

class TestCalcFunctions(unittest.TestCase):
    def test_add(self):
        self.assertEqual(add(1, 2), 3)
        self.assertEqual(add(-1, 1), 0)
        self.assertEqual(add(-1, -1), -2)
    def test_sub(self):
        self.assertEqual(sub(1, 2), -1)
        self.assertEqual(sub(-1, 1), -2)
        self.assertEqual(sub(-1, -1), 0)
    def test_mul(self):
        self.assertEqual(mul(1, 2), 2)
        self.assertEqual(mul(-1, 1), -1)
        self.assertEqual(mul(-1, -1), 1)
    def test_div(self):
        self.assertEqual(div(1, 2), 0.5)
        self.assertEqual(div(-1, 1), -1)
        self.assertEqual(div(-1, -1), 1)
        with self.assertRaises(ZeroDivisionError):
            div(1, 0)

if __name__ == '__main__':
    unittest.main()