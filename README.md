# Data Set: Finding Unsafe Go Code in the Wild

This is a fork of [stg-tud/unsafe_go_study_results](https://github.com/stg-tud/unsafe_go_study_results) which adds a [CFG generator](./scripts/data-acquisition-tool/cfg/cfg.go) to the data-acquisition-tool.

The CFGs created for the unsafe usage dataset in [`labeled-usages-dataset`](./labeled-usages-dataset/) can be found in [`labeled-cfg-dataset`](./labeled-cfg-dataset/).

To regenerate the CFGs from scratch, you must first build the data-acquision-tool (see the [README](./scripts/data-acquisition-tool/README.md)).
Then execute `./create_cfgs.clj -r` from the `scripts` directory;
for this the [Babashka](https://babashka.org/) runtime must be installed.

## License

All project and dependency code is licensed under the terms of the respective licenses for the specific projects.

<a rel="license" href="http://creativecommons.org/licenses/by-nc-nd/4.0/"><img alt="Creative Commons Lizenzvertrag" style="border-width:0" src="https://i.creativecommons.org/l/by-nc-nd/4.0/88x31.png" /></a><br />Our study material and data set is licensed under a <a rel="license" href="http://creativecommons.org/licenses/by-nc-nd/4.0/">Creative Commons Attribution-NonCommercial-NoDerivs  4.0 International License</a>.
