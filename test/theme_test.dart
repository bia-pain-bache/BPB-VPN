import 'package:bpb/models/theme_color.dart';
import 'package:bpb/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets(
    'theme scales Baloo text styles without failing assertions or scaling explicit monospace',
    (tester) async {
      double? bodySize;
      double? defaultTextSize;
      double? monoSize;

      for (final brightness in Brightness.values) {
        await tester.pumpWidget(
          MaterialApp(
            theme: buildTheme(brightness, ThemeColor.blue),
            builder: (context, child) => Theme(
              data: scaleThemeText(Theme.of(context)),
              child: child ?? const SizedBox.shrink(),
            ),
            home: Scaffold(
              body: Builder(
                builder: (context) {
                  bodySize = Theme.of(context).textTheme.bodyMedium?.fontSize;
                  defaultTextSize = DefaultTextStyle.of(context).style.fontSize;
                  monoSize = const TextStyle(fontFamily: 'monospace', fontSize: 12).fontSize;
                  return const Column(
                    children: [
                      Text('Baloo'),
                      Text('Mono', style: TextStyle(fontFamily: 'monospace', fontSize: 12)),
                    ],
                  );
                },
              ),
            ),
          ),
        );

        expect(bodySize, closeTo(14 * 1.1, 0.01));
        expect(defaultTextSize, closeTo(14 * 1.1, 0.01));
        expect(monoSize, 12);
      }
    },
  );
}
