// Greeting helper, kept separate so the CLI banner can import it alone.
function greet(name) {
  return `Hello, ${name}!`;
}

module.exports = { greet };
