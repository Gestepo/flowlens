import { expect, test } from '@playwright/test'

const viewports = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'compact-desktop', width: 1024, height: 768 },
  { name: 'mobile', width: 390, height: 844 },
  { name: 'narrow-mobile', width: 360, height: 800 },
]

for (const viewport of viewports) {
  test(`overview renders at ${viewport.name}`, async ({ page }) => {
    await page.setViewportSize(viewport)
    await page.goto('/')
    await expect(page.getByRole('heading', { name: '流量概览' })).toBeVisible()
    await expect(page.getByText('入站流量', { exact: true })).toBeVisible()
    await expect(page.getByText('出站流量', { exact: true })).toBeVisible()
    const chart = page.getByRole('img', { name: '入站和出站流量趋势' })
    await expect(chart).toBeVisible()
    const chartBox = await chart.boundingBox()
    expect(chartBox?.width).toBeGreaterThan(200)
    expect(chartBox?.height).toBeGreaterThan(180)
    const plottedLines = chart.locator('.recharts-line-curve')
    await expect(plottedLines).toHaveCount(2)
    const paths = await plottedLines.evaluateAll((lines) => lines.map((line) => line.getAttribute('d') ?? ''))
    expect(paths.every((path) => path.length > 20 && !path.includes('NaN'))).toBe(true)
    const accessibleData = await page.locator('.traffic-chart > .sr-only').boundingBox()
    expect(accessibleData?.height).toBeLessThanOrEqual(1)
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    expect(overflow).toBe(0)
    await page.screenshot({ path: `test-results/overview-${viewport.name}.png`, fullPage: true })
  })
}
