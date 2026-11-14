const button = document.querySelector("#check-health");
const status = document.querySelector("#health-status");

button.addEventListener("click", async () => {
  button.disabled = true;
  status.textContent = "Checking…";

  try {
    const response = await fetch("/health");
    if (!response.ok) throw new Error(`HTTP ${response.status}`);

    const body = await response.json();
    if (body.status !== "ok") throw new Error("Unexpected health response");
    status.textContent = "Connected";
  } catch (error) {
    status.textContent = `Connection failed: ${error.message}`;
  } finally {
    button.disabled = false;
  }
});
