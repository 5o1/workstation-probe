// charts.js — thin wrapper around Chart.js. One Chart instance per canvas;
// reused across updates via chart.update('none'). The CDN import is loaded
// once and cached by the browser; switch to a vendored copy by changing
// this URL and serving the file under
// /static/workstation-probe/js/vendor/.
import {
  Chart,
  registerables,
} from 'https://cdn.jsdelivr.net/npm/chart.js@4.4.6/+esm';

Chart.register(...registerables);

const charts = new WeakMap();

const BASE_OPTIONS = {
  responsive: true,
  maintainAspectRatio: false,
  animation: { duration: 250 },
  plugins: { legend: { display: false }, tooltip: { enabled: false } },
  scales: {
    x: { display: false, type: 'linear' },
    y: {
      min: 0,
      max: 100,
      ticks: { font: { size: 10 }, callback: (v) => `${v}%` },
      grid: { color: 'rgba(127,127,127,0.15)' },
    },
  },
  elements: {
    point: { radius: 0 },
    line: { borderWidth: 1.5, tension: 0.25 },
  },
};

export function ensureChart(canvas, dataset) {
  const data = {
    datasets: [
      {
        data: dataset.points, // [{x: epochMs, y: number}, ...]
        borderColor: dataset.color,
        backgroundColor: dataset.color + '22',
        fill: true,
      },
    ],
  };
  let chart = charts.get(canvas);
  if (!chart) {
    chart = new Chart(canvas.getContext('2d'), {
      type: 'line',
      data,
      options: { ...BASE_OPTIONS, scales: { ...BASE_OPTIONS.scales, y: { ...BASE_OPTIONS.scales.y, max: dataset.max ?? 100 } } },
    });
    charts.set(canvas, chart);
  } else {
    chart.data = data;
    chart.options.scales.y.max = dataset.max ?? 100;
    chart.update('none');
  }
  return chart;
}