document.documentElement.classList.remove("no-js");

const filter = document.querySelector("[data-browser-filter]");
if (filter instanceof HTMLInputElement) {
  const items = document.querySelectorAll("[data-filter-item]");
  const groups = document.querySelectorAll("[data-filter-group]");
  const sections = document.querySelectorAll("[data-filter-section]");
  const noResults = document.querySelector("[data-no-results]");
  filter.addEventListener("input", () => {
    const query = filter.value.trim().toLocaleLowerCase();
    let visibleItems = 0;
    for (const item of items) {
      const hidden = query !== "" &&
        !item.textContent.toLocaleLowerCase().includes(query);
      item.toggleAttribute("hidden", hidden);
      if (!hidden) {
        visibleItems++;
      }
    }
    for (const group of groups) {
      const hasVisibleItem = group.querySelector(
        "[data-filter-item]:not([hidden])",
      ) !== null;
      group.toggleAttribute("hidden", query !== "" && !hasVisibleItem);
    }
    const empty = query !== "" && visibleItems === 0;
    for (const section of sections) {
      const hasVisibleItem = section.querySelector(
        "[data-filter-item]:not([hidden])",
      ) !== null;
      section.toggleAttribute("hidden", query !== "" && !hasVisibleItem);
    }
    if (noResults instanceof HTMLElement) {
      noResults.toggleAttribute("hidden", !empty);
    }
  });
}

document.addEventListener("submit", async (event) => {
  const form = event.target;
  if (
    !(form instanceof HTMLFormElement) || !form.matches("[data-http-example]")
  ) {
    return;
  }
  event.preventDefault();

  const urlField = form.elements.namedItem("url");
  const bodyField = form.elements.namedItem("body");
  const submit = form.querySelector('button[type="submit"]');
  const output = form.querySelector("[data-response]");
  const method = form.dataset.method;
  if (
    !(urlField instanceof HTMLInputElement) ||
    !(submit instanceof HTMLButtonElement) ||
    !(output instanceof HTMLPreElement) ||
    method === undefined
  ) {
    return;
  }

  output.hidden = false;
  output.textContent = "Sending…";
  submit.disabled = true;
  try {
    const requestURL = new URL(urlField.value, window.location.href);
    if (requestURL.origin !== window.location.origin) {
      throw new Error("Use this service's origin.");
    }
    const upperMethod = method.toUpperCase();
    if (["POST", "PUT", "PATCH", "DELETE"].includes(upperMethod)) {
      const confirmMessage = `Send ${upperMethod} to ${requestURL.href}?`;
      if (!window.confirm(confirmMessage)) {
        output.hidden = true;
        submit.disabled = false;
        return;
      }
    }
    const options = { method };
    if (bodyField instanceof HTMLTextAreaElement) {
      options.headers = { "Content-Type": "application/json" };
      options.body = bodyField.value;
    }
    const response = await fetch(requestURL, options);
    const body = await response.text();
    let formattedBody = body;
    const contentType = response.headers.get("Content-Type");
    if (body && contentType?.includes("application/json")) {
      try {
        formattedBody = JSON.stringify(JSON.parse(body), null, 2);
      } catch {
        // Show invalid JSON as-is.
      }
    }
    output.textContent = `HTTP ${response.status} ${response.statusText}` +
      (formattedBody ? `\n\n${formattedBody}` : "");
  } catch (error) {
    output.textContent = error instanceof Error
      ? error.message
      : "Request failed";
  } finally {
    submit.disabled = false;
  }
});
