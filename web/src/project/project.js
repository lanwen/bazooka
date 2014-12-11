"use strict";

angular.module('bzk.project', ['bzk.utils', 'ngRoute']);

angular.module('bzk.project').config(function($routeProvider){
	$routeProvider.when('/:pid', {
			templateUrl: 'project/project.html',
			controller: 'ProjectController',
			reloadOnSearch: false
		});
});

angular.module('bzk.project').factory('ProjectResource', function($http){
	return {
		fetch: function(id) {
			return $http.get('/api/project/'+id);
		},
		jobs: function (id) {
			return $http.get('/api/project/'+id+'/job');
		},
		job: function (id) {
			return $http.get('/api/job/'+id);
		},
		variants: function (jid) {
			return $http.get('/api/job/'+jid+'/variant');
		},
		build: function (id) {
			return $http.post('/api/project/'+id+'/job', {
				reference: 'master'
			});
		},
		variantLog: function (vid) {
			return $http.get('/api/variant/'+vid+'/log');
		}
	};
});

angular.module('bzk.project').controller('ProjectController', function($scope, $routeParams, ProjectResource){
	var pId = $routeParams.pid;

	ProjectResource.fetch(pId).success(function(project){
		console.log(project);
		$scope.project = project;
	});
});

angular.module('bzk.project').controller('JobsController', function($scope, ProjectResource, $routeParams, $location, $interval){
	var pId = $routeParams.pid;
	$scope.refreshJobs = function() {
		ProjectResource.jobs(pId).success(function(jobs){
			$scope.jobs = jobs;
		});
	};

	$scope.refreshJobs();

	$scope.buildProject = function() {
		ProjectResource.build($scope.project.id).success(function(){
			$scope.refreshJobs();
		});
	};

	$scope.isSelected = function(j) {
		return j.id===$location.search().j;
	};

	$interval(function() {
		$scope.refreshJobs();
	}, 3000);
});

angular.module('bzk.project').controller('JobController', function($scope, ProjectResource, $location, $interval){
	var jId;
	var refreshPromise;

	$scope.variantSelected = function() {
		return $location.search().v;
	};
	function refreshVariants() {
		ProjectResource.variants(jId).success(function(variants){
			$scope.variants = variants;
			setupMeta(variants);

			if(_.findWhere($scope.variants, {status: 'RUNNING'})) {

				if(!refreshPromise) {

					refreshPromise= $interval(refreshVariants, 3000);
				}
			} else if(refreshPromise) {

				$interval.cancel(refreshPromise);
				refreshPromise=null;
			}
		});
	}

	function refresh() {
		jId = $location.search().j;
  		if(jId) {
			ProjectResource.job(jId).success(function(job){
				$scope.job = job;
			});
			refreshVariants();
		}
	}

	function setupMeta(variants) {
		var colorsDb = ['#4a148c' /* Purple */,
	'#006064' /* Cyan */,
	'#f57f17' /* Yellow */,
	'#e65100' /* Orange */,
	'#263238' /* Blue Grey */,
	'#b71c1c' /* Red */,
	'#1a237e' /* Indigo */,
	'#1b5e20' /* Green */,
	'#33691e' /* Light Green */,
	'#212121' /* Grey 500 */,
	'#880e4f' /* Pink */,
	'#311b92' /* Deep Purple */,
	'#01579b' /* Light Blue */,
	'#004d40' /* Teal */,
	'#ff6f00' /* Amber */,
	'#bf360c' /* Deep Orange */,
	'#0d47a1' /* Blue */,
	'#827717' /* Lime */,
	'#3e2723' /* Brown 500 */,
	'#000000'];

		var metaLabels = [], colors={};
		if (variants.length>0) {
			var vref = variants[0];
			_.each(vref.metas, function (m) {
				metaLabels.push(m.kind=='env'?'$'+m.name:m.name);
			});

			_.each(vref.metas, function(m, i){
				var mcolors={};
				colors[m.name] = mcolors;
				var colIdx=0;
				_.each(variants, function (v) {
					var val=v.metas[i].value;
					if (!mcolors[val]) {
						mcolors[val] = colorsDb[colIdx];
						if(colIdx<colorsDb.length-1) {
							colIdx++;
						}
					}
				});
			});

		}
		
		$scope.metaLabels=metaLabels;
		$scope.metaColors=colors;
	}

	$scope.metaColor = function(vmeta) {
		return $scope.metaColors[vmeta.name][vmeta.value];
	};

	$scope.$on('$routeUpdate', refresh);

	refresh();
});

angular.module('bzk.project').controller('VariantsController', function($scope, ProjectResource, bzkScroll, $location, $timeout, $interval){
	$scope.isSelected = function(v) {
		return v.id===$location.search().v;
	};

	function loadLogs() {
		var vId = $location.search().v;
  		if(vId) {
  			$scope.logger.prepare();
  			bzkScroll.toTheRight();

			ProjectResource.variantLog(vId).success(function(logs){
				// console.log('got logs', logs)
				// $scope.logs = logs;

				$scope.logger.finish(logs);


			});
		}
	}

	// yield to let give bzkLog directive time to set its sink in the scope
	$timeout(loadLogs);

	$scope.$on('$routeUpdate', function(){
		loadLogs();
	});
});

angular.module('bzk.project').directive('bzkLog', function(){
	return {
		restrict: 'A',
		scope: {
			sink: '=bzkLog'
		},
		template: '<img class="loading" src="/images/loading.gif" ng-if="loading"></img>',
		link: function($scope, elem, attrs) {
			var row = 1;
			$(elem).append('<pre></pre>');
			var into = $(elem).find('pre');
			$scope.sink = {
				prepare: function() {
					this.clear();
					$scope.loading = true;
				},
				finish: function(lines) {
					this.append(lines);
					$scope.loading=false;
				},
				append: function(lines) {
					var data = '';
					_.each(lines, function(line){
						data += '<p><span>'+row+'</span>'+line.msg+'</p>';
						row++;
					});
					into.append(data);
				},
				clear: function(){
					row = 1;
					into.empty();
					into.scrollTop(0);
				}
			};
		}
	};
});
