// Small arithmetic helpers shared by the report builders. Kept deliberately
// dependency-free so it can be imported from both the CLI and the browser
// bundle; anything that needs Intl formatting belongs in format.js instead
// of here.

function add(a, b) {
  return a + b;
}

function subtract(a, b) {
  return a - b;
}

function multiply(a, b) {
  return a * b;
}

function divide(a, b) {
  if (b === 0) {
    throw new Error("division by zero");
  }
  return a / b;
}

function average(values) {
  if (values.length === 0) {
    return 0;
  }
  const sum = values.reduce((acc, v) => acc + v, 0);
  return sum / values.length;
}

module.exports = {
  add,
  subtract,
  multiply,
  divide,
  average,
};
