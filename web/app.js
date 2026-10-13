/* hic — page behaviour.
 *
 * Charts read their colours from the same token layer the CSS uses, so switching
 * theme is one attribute flip and a redraw, never a second palette in JS. Every
 * value that reaches a chart comes from getComputedStyle, never a literal.
 */

const Chart = window.Chart;

const PAGE_SIZE = 20;

const MONTHS = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
];

/* Category to chart-series token index. A category keeps the same colour in the
   panel and in the trend, so a hue always means one thing. "Normal" exists in both
   dimensions and so takes a different index in each. */
const TEXTURE_SERIES = { Soft: 3, Normal: 1, Hard: 2 };
const SMELL_SERIES = { Strong: 4, Normal: 5 };
const SPARE_SERIES = [6, 7, 8];

const els = {
  form: document.getElementById("create-record"),
  tbody: document.getElementById("records-body"),
  recordsSection: document.getElementById("records-section"),
  recordsCount: document.getElementById("records-count"),
  breakdown: document.getElementById("breakdown"),
  trend: document.getElementById("trend"),
  emptyState: document.getElementById("empty-state"),
  emptyCta: document.getElementById("empty-cta"),
  pageInfo: document.getElementById("page-info"),
  prev: document.getElementById("prev"),
  next: document.getElementById("next"),
  status: document.getElementById("status"),
  statusText: document.getElementById("status-text"),
  loadStatus: document.getElementById("load-status"),
  figure: document.getElementById("days-figure"),
  caption: document.getElementById("days-caption"),
  rhythmStatus: document.getElementById("rhythm-status"),
  rhythmStatusText: document.getElementById("rhythm-status-text"),
  statTotal: document.getElementById("stat-total"),
  statYear: document.getElementById("stat-year"),
  statCommon: document.getElementById("stat-common"),
  sideNote: document.getElementById("side-note"),
  footMeta: document.getElementById("foot-meta"),
  trendYear: document.getElementById("trend-year"),
  trendLegend: document.getElementById("trend-legend"),
  textureTotal: document.getElementById("texture-total"),
  smellTotal: document.getElementById("smell-total"),
  textureLegend: document.getElementById("texture-legend"),
  smellLegend: document.getElementById("smell-legend"),
  textureChart: document.getElementById("texture-chart"),
  smellChart: document.getElementById("smell-chart"),
  ytdChart: document.getElementById("ytd-chart"),
  themeToggle: document.getElementById("theme-toggle"),
  themeIcon: document.getElementById("theme-icon"),
  themeLabel: document.getElementById("theme-label"),
  dialog: document.getElementById("confirm-delete"),
  dialogBody: document.getElementById("confirm-body"),
};

let records = [];
let total = 0;
let page = 0;
let pendingDeleteId = null;
let textureChart = null;
let smellChart = null;
let ytdChart = null;

const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

/* ---------- tokens ---------- */

function token(name, fallback) {
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return value || fallback;
}

function seriesColor(index) {
  return token(`--color-chart-${index}`, "#64748b");
}

/* ---------- helpers ---------- */

function formatDateTime(value) {
  const date = new Date(value);
  return date.toLocaleString(undefined, {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatNumber(value) {
  return value.toLocaleString();
}

function plural(count, one, many) {
  return count === 1 ? one : many;
}

function summarise(counts, known, getName) {
  /* Returns [{ key, label, value }] with known categories first, in a fixed
     order, then any extra category the data carries. */
  const keys = [...known, ...Object.keys(counts).filter((key) => !known.includes(key))];
  return keys
    .map((key) => ({ key, label: getName(key), value: counts[key] || 0 }))
    .filter((entry) => entry.value > 0);
}

function colourFor(key, knownMap, used) {
  if (knownMap[key]) {
    used.add(knownMap[key]);
    return seriesColor(knownMap[key]);
  }
  const spare = SPARE_SERIES.find((index) => !used.has(index)) || SPARE_SERIES.length + used.size;
  used.add(spare);
  return seriesColor(spare);
}

async function request(url, options) {
  const res = await fetch(url, options);
  if (!res.ok) {
    throw new Error(`${res.status} ${res.statusText}`);
  }
  return res;
}

async function fetchJSON(url) {
  return (await request(url)).json();
}

function currentYear() {
  return String(new Date().getFullYear());
}

function showError(message) {
  els.statusText.textContent = message;
  els.status.hidden = false;
}

function clearError() {
  els.status.hidden = true;
  els.statusText.textContent = "";
}

/* ---------- theme ---------- */

function applyTheme(theme, { redraw = true } = {}) {
  const dark = theme === "dark";
  document.documentElement.setAttribute("data-theme", theme);
  els.themeToggle.setAttribute("aria-pressed", String(dark));
  els.themeIcon.setAttribute("href", dark ? "#i-sun" : "#i-moon");
  els.themeLabel.textContent = dark
    ? "Switch to the light theme"
    : "Switch to the dark theme";
  try {
    localStorage.setItem("hic-theme", theme);
  } catch (err) {
    /* Storage can be blocked. The theme still applies for this session. */
  }
  if (redraw) {
    redrawCharts();
  }
}

els.themeToggle.addEventListener("click", () => {
  const current = document.documentElement.getAttribute("data-theme");
  applyTheme(current === "dark" ? "light" : "dark");
});

function redrawCharts() {
  if (lastStats) {
    renderCharts(lastStats);
  }
}

/* ---------- data ---------- */

let lastStats = null;

async function loadRecords({ resetPage = false } = {}) {
  if (resetPage) {
    page = 0;
  }

  els.loadStatus.hidden = false;

  try {
    const [pageData, stats] = await Promise.all([
      fetchJSON(`/api/records?limit=${PAGE_SIZE}&offset=${page * PAGE_SIZE}`),
      fetchJSON(`/api/stats?year=${currentYear()}`),
    ]);

    const pages = Math.max(1, Math.ceil(pageData.total / PAGE_SIZE));
    if (page > pages - 1) {
      page = pages - 1;
      return loadRecords();
    }

    records = pageData.records;
    total = pageData.total;
    lastStats = stats;
    clearError();
    els.loadStatus.hidden = true;
    render();
    renderSummary(stats);
    renderCharts(stats);
  } catch (err) {
    els.loadStatus.hidden = true;
    showError(`Could not load Clover's log. ${err.message}. Try reloading the page.`);
  }
}

/* ---------- hero and stats ---------- */

function renderSummary(stats) {
  const days = stats.daysSinceLast;
  const yearCount = Object.values(stats.textureMonths || {}).reduce(
    (sum, monthCounts) =>
      sum + Object.values(monthCounts).reduce((inner, value) => inner + value, 0),
    0,
  );

  if (total === 0) {
    els.figure.textContent = "--";
    els.caption.textContent = "No visits logged yet. Add the first one below.";
    setRhythm("neutral", "The first visit starts the count");
    els.statCommon.textContent = "Nothing yet";
    els.sideNote.textContent = "Log a visit and hic will fill this panel in.";
  } else {
    els.figure.textContent = formatNumber(days ?? 0);
    els.caption.textContent = `${plural(days ?? 0, "day", "days")} since Clover's last visit`;
    if ((days ?? 0) <= 1) {
      setRhythm("success", "Logged within the last day");
    } else if ((days ?? 0) === 2) {
      setRhythm("neutral", "Two days since the last visit");
    } else {
      setRhythm("warning", "Three or more days since the last visit");
    }

    const top = Object.entries(stats.texture || {}).sort((a, b) => b[1] - a[1])[0];
    els.statCommon.textContent = top && top[0] ? top[0] : "Nothing yet";
    els.sideNote.textContent = `${formatNumber(total)} ${plural(total, "visit", "visits")} on file, ${formatNumber(yearCount)} of them in ${currentYear()}.`;
  }

  els.statTotal.textContent = formatNumber(total);
  els.statYear.textContent = formatNumber(yearCount);
}

const RHYTHM_ICON = {
  success: "#i-check-circle",
  warning: "#i-alert-triangle",
  neutral: "#i-info",
};

function setRhythm(kind, message) {
  els.rhythmStatus.className = `status status-${kind}`;
  els.rhythmStatus.querySelector("use").setAttribute("href", RHYTHM_ICON[kind]);
  els.rhythmStatusText.textContent = message;
}

/* ---------- records table ---------- */

function valueTag(value) {
  /* Neutral on purpose: the word carries the value, and colour is reserved for
     the charts, where a hue has to map to a series. Redundant colour in a 110-row
     table is noise, not information. */
  const span = document.createElement("span");
  span.className = "tag";
  span.dataset.v = value || "Unspecified";
  span.textContent = value || "Unspecified";
  return span;
}

function render() {
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  if (total === 0) {
    els.emptyState.hidden = false;
    els.breakdown.hidden = true;
    els.trend.hidden = true;
    els.recordsSection.hidden = true;
    els.footMeta.textContent = "Waiting for the first visit";
    return;
  }

  els.emptyState.hidden = true;
  els.breakdown.hidden = false;
  els.trend.hidden = false;
  els.recordsSection.hidden = false;

  els.tbody.replaceChildren(
    ...records.map((record) => {
      const row = document.createElement("tr");

      const when = document.createElement("td");
      when.className = "numeric";
      when.dataset.v = record.Datetime;
      when.textContent = formatDateTime(record.Datetime);
      row.append(when);

      const texture = document.createElement("td");
      texture.append(valueTag(record.Texture));
      row.append(texture);

      const smell = document.createElement("td");
      smell.append(valueTag(record.Smell));
      row.append(smell);

      const comments = document.createElement("td");
      comments.className = "cell-comments";
      comments.dataset.v = record.Comments || "";
      comments.textContent = record.Comments || "No comment";
      row.append(comments);

      const actions = document.createElement("td");
      const button = document.createElement("button");
      button.type = "button";
      button.className = "button button-quiet";
      button.dataset.id = String(record.ID);
      button.innerHTML =
        '<svg class="icon" aria-hidden="true"><use href="#i-trash" /></svg>';
      const label = document.createElement("span");
      label.className = "sr-only";
      label.textContent = `Delete the visit from ${formatDateTime(record.Datetime)}`;
      button.append(label);
      button.addEventListener("click", () => confirmDelete(record));
      actions.append(button);
      row.append(actions);

      return row;
    }),
  );

  const first = page * PAGE_SIZE + 1;
  const last = Math.min(total, page * PAGE_SIZE + records.length);
  els.recordsCount.textContent = `${formatNumber(total)} ${plural(total, "visit", "visits")}`;
  els.pageInfo.textContent = `Page ${page + 1} of ${pages}`;
  els.prev.disabled = page === 0;
  els.next.disabled = page >= pages - 1;
  els.footMeta.textContent = `Showing ${formatNumber(first)} to ${formatNumber(last)} of ${formatNumber(total)} ${plural(total, "visit", "visits")}`;
}

/* Sorting runs on the rendered rows, which is what the heading promises. */
document.querySelectorAll(".sort").forEach((button) => {
  button.addEventListener("click", () => {
    const th = button.closest("th");
    const column = Number(button.dataset.col);
    const ascending = th.getAttribute("aria-sort") !== "ascending";

    document
      .querySelectorAll("thead th")
      .forEach((other) => other.setAttribute("aria-sort", "none"));
    th.setAttribute("aria-sort", ascending ? "ascending" : "descending");

    const rows = [...els.tbody.rows];
    rows.sort((a, b) => {
      /* Every column carries a sortable data-v: an ISO timestamp for When, the
         label for the text columns. Both compare as strings, and ISO dates sort
         as strings correctly. */
      const left = a.cells[column].dataset.v ?? a.cells[column].textContent.trim();
      const right = b.cells[column].dataset.v ?? b.cells[column].textContent.trim();
      const result = String(left).localeCompare(String(right));
      return ascending ? result : -result;
    });
    rows.forEach((row) => els.tbody.append(row));
  });
});

/* ---------- delete ---------- */

function confirmDelete(record) {
  pendingDeleteId = record.ID;
  els.dialogBody.textContent = `The visit from ${formatDateTime(record.Datetime)} will be removed from the log. This cannot be undone.`;
  els.dialog.showModal();
}

els.dialog.addEventListener("close", () => {
  if (els.dialog.returnValue === "confirm" && pendingDeleteId !== null) {
    deleteRecord(pendingDeleteId);
  }
  pendingDeleteId = null;
});

async function deleteRecord(id) {
  try {
    await request(`/api/records/${id}`, { method: "DELETE" });
    clearError();
    await loadRecords();
  } catch (err) {
    showError(`Could not delete that visit. ${err.message}. Try again.`);
  }
}

/* ---------- pagination ---------- */

els.prev.addEventListener("click", () => {
  page -= 1;
  loadRecords();
});

els.next.addEventListener("click", () => {
  page += 1;
  loadRecords();
});

els.emptyCta.addEventListener("click", () => {
  const first = els.form.querySelector('input[name="texture"]');
  if (first) {
    first.focus();
  }
  els.form.scrollIntoView({ behavior: reducedMotion.matches ? "auto" : "smooth" });
});

/* ---------- create ---------- */

els.form.addEventListener("submit", async (event) => {
  event.preventDefault();

  const data = new FormData(els.form);
  const submit = els.form.querySelector('button[type="submit"]');

  try {
    submit.setAttribute("aria-busy", "true");
    submit.disabled = true;
    await request("/api/records", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        texture: data.get("texture"),
        smell: data.get("smell"),
        comments: data.get("comments") || "",
      }),
    });
    els.form.reset();
    clearError();
    await loadRecords({ resetPage: true });
  } catch (err) {
    showError(`Could not save that visit. ${err.message}. Try again.`);
  } finally {
    submit.removeAttribute("aria-busy");
    submit.disabled = false;
  }
});

/* ---------- charts ---------- */

function chartOptions() {
  return {
    responsive: true,
    maintainAspectRatio: false,
    animation: reducedMotion.matches
      ? false
      : { duration: Number.parseInt(token("--duration-slow"), 10) || 400 },
    color: token("--color-text-secondary"),
    plugins: {
      legend: { display: false },
      tooltip: {
        backgroundColor: token("--color-surface-inverse"),
        titleColor: token("--color-text-on-inverse"),
        bodyColor: token("--color-text-on-inverse"),
        borderColor: token("--color-border-inverse"),
        borderWidth: 1,
        padding: 10,
        displayColors: true,
      },
    },
  };
}

function legendItem(colour, label, value, share, { track = true } = {}) {
  const li = document.createElement("li");
  li.className = "legend-item";

  const swatch = document.createElement("span");
  swatch.className = "legend-swatch";
  swatch.setAttribute("aria-hidden", "true");
  swatch.style.background = colour;

  const name = document.createElement("span");
  name.className = "legend-name";
  name.textContent = label;

  const count = document.createElement("span");
  count.className = "legend-count";
  count.textContent = `${formatNumber(value)}${share != null ? ` · ${share}%` : ""}`;

  li.append(swatch, name, count);

  if (track && share != null) {
    const rail = document.createElement("span");
    rail.className = "legend-track";
    const fill = document.createElement("span");
    fill.className = "legend-fill";
    fill.style.inlineSize = `${share}%`;
    fill.style.background = colour;
    rail.append(fill);
    li.append(rail);
  }

  return li;
}

function renderLegend(target, entries, totalValue, track) {
  target.replaceChildren(
    ...entries.map((entry) => {
      const share = totalValue > 0 ? Math.round((entry.value / totalValue) * 100) : 0;
      return legendItem(entry.colour, entry.label, entry.value, share, { track });
    }),
  );
}

function pieEntries(counts, knownMap, knownOrder, getLabel) {
  const used = new Set();
  const entries = summarise(counts, knownOrder, getLabel).map((entry) => ({
    label: `${entry.label}`,
    value: entry.value,
    colour: colourFor(entry.key, knownMap, used),
  }));
  return entries;
}

function doughnutConfig(entries) {
  return {
    type: "doughnut",
    data: {
      labels: entries.map((entry) => entry.label),
      datasets: [
        {
          data: entries.map((entry) => entry.value),
          backgroundColor: entries.map((entry) => entry.colour),
          borderWidth: 0,
          hoverOffset: 0,
        },
      ],
    },
    options: { ...chartOptions(), cutout: "68%" },
  };
}

function renderChart(instance, canvas, config) {
  if (!Chart) {
    return null;
  }
  if (instance) {
    instance.destroy();
  }
  return new Chart(canvas, config);
}

function describe(entries, noun, totalValue) {
  if (entries.length === 0) {
    return `${noun} split, no data yet`;
  }
  const parts = entries.map((entry) => {
    const share = totalValue > 0 ? Math.round((entry.value / totalValue) * 100) : 0;
    return `${entry.label} ${entry.value} visits, ${share} percent`;
  });
  return `${noun} split across ${formatNumber(totalValue)} visits: ${parts.join("; ")}`;
}

function renderCharts(stats) {
  const textureEntries = pieEntries(
    stats.texture || {},
    TEXTURE_SERIES,
    ["Soft", "Normal", "Hard"],
    (key) => key || "Unspecified",
  );
  const smellEntries = pieEntries(
    stats.smell || {},
    SMELL_SERIES,
    ["Strong", "Normal"],
    (key) => key || "Unspecified",
  );

  const textureSum = textureEntries.reduce((sum, entry) => sum + entry.value, 0);
  const smellSum = smellEntries.reduce((sum, entry) => sum + entry.value, 0);

  els.textureTotal.textContent = formatNumber(textureSum);
  els.smellTotal.textContent = formatNumber(smellSum);

  renderLegend(els.textureLegend, textureEntries, textureSum, true);
  renderLegend(els.smellLegend, smellEntries, smellSum, true);

  els.textureChart.setAttribute(
    "aria-label",
    describe(textureEntries, "Texture", textureSum),
  );
  els.smellChart.setAttribute("aria-label", describe(smellEntries, "Smell", smellSum));

  if (!Chart) {
    return;
  }

  textureChart = renderChart(
    textureChart,
    els.textureChart,
    doughnutConfig(textureEntries),
  );
  smellChart = renderChart(smellChart, els.smellChart, doughnutConfig(smellEntries));

  renderTrend(stats);
}

function monthCategories(months, known) {
  const names = new Set(known);
  for (const monthCounts of Object.values(months || {})) {
    for (const name of Object.keys(monthCounts)) {
      names.add(name);
    }
  }
  return [...names];
}

function trendDatasets(textureMonths, smellMonths) {
  const year = currentYear();
  const monthsSoFar = new Date().getMonth() + 1;
  const used = new Set();
  const datasets = [];

  const textureNames = monthCategories(textureMonths, ["Soft", "Normal", "Hard"]);
  const smellNames = monthCategories(smellMonths, ["Strong", "Normal"]);

  for (const name of textureNames) {
    datasets.push(
      trendDataset({
        label: `Texture: ${name || "Unspecified"}`,
        year,
        monthsSoFar,
        months: textureMonths,
        name,
        colour: colourFor(name, TEXTURE_SERIES, used),
      }),
    );
  }

  for (const name of smellNames) {
    datasets.push(
      trendDataset({
        label: `Smell: ${name || "Unspecified"}`,
        year,
        monthsSoFar,
        months: smellMonths,
        name,
        colour: colourFor(name, SMELL_SERIES, used),
      }),
    );
  }

  return datasets;
}

function trendDataset({ label, year, monthsSoFar, months, name, colour }) {
  return {
    label,
    data: Array.from({ length: monthsSoFar }, (_, i) => {
      const key = `${year}-${String(i + 1).padStart(2, "0")}`;
      return months?.[key]?.[name] || 0;
    }),
    borderColor: colour,
    backgroundColor: colour,
    pointBackgroundColor: colour,
    pointRadius: 2,
    borderWidth: 2,
    tension: 0.3,
    fill: false,
  };
}

function trendConfig(datasets) {
  const year = currentYear();
  const monthsSoFar = new Date().getMonth() + 1;
  const labels = MONTHS.slice(0, monthsSoFar);

  return {
    type: "line",
    data: { labels, datasets },
    options: {
      ...chartOptions(),
      interaction: { mode: "index", intersect: false },
      scales: {
        x: {
          grid: { display: false },
          border: { color: token("--color-chart-axis-line") },
          ticks: { color: token("--color-chart-axis-label") },
        },
        y: {
          beginAtZero: true,
          grid: { color: token("--color-chart-grid") },
          border: { display: false },
          ticks: {
            color: token("--color-chart-axis-label"),
            precision: 0,
          },
        },
      },
      plugins: {
        ...chartOptions().plugins,
        tooltip: {
          ...chartOptions().plugins.tooltip,
          callbacks: {
            title: (items) => (items.length ? `${items[0].label} ${year}` : ""),
          },
        },
      },
    },
  };
}

function renderTrend(stats) {
  const datasets = trendDatasets(stats.textureMonths || {}, stats.smellMonths || {});
  els.trendYear.textContent = currentYear();

  els.trendLegend.replaceChildren(
    ...datasets.map((dataset) => {
      const sum = dataset.data.reduce((acc, value) => acc + value, 0);
      return legendItem(dataset.borderColor, dataset.label, sum, null, { track: false });
    }),
  );

  const yearTotals = MONTHS.slice(0, new Date().getMonth() + 1).map((month, i) => {
    const key = `${currentYear()}-${String(i + 1).padStart(2, "0")}`;
    const total = datasets.reduce(
      (sum, dataset) => sum + (dataset.data[i] || 0),
      0,
    );
    return `${month} ${total}`;
  });
  els.ytdChart.setAttribute(
    "aria-label",
    `Visits per month in ${currentYear()}: ${yearTotals.join(", ")}`,
  );

  if (!Chart) {
    return;
  }

  ytdChart = renderChart(ytdChart, els.ytdChart, trendConfig(datasets));
}

/* ---------- boot ---------- */

applyTheme(
  document.documentElement.getAttribute("data-theme") || "light",
  { redraw: false },
);

if (reducedMotion.addEventListener) {
  reducedMotion.addEventListener("change", () => redrawCharts());
}

loadRecords();
